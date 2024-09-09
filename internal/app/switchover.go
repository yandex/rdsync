package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

const (
	switchoverVersion = 1
)

func countAliveHAReplicasWithinNodes(nodes []string, shardState map[string]*HostState) int {
	cnt := 0
	for _, hostname := range nodes {
		state, ok := shardState[hostname]
		if ok && state.PingOk && state.PingStable && state.ReplicaState != nil {
			cnt++
		}
	}
	return cnt
}

func (app *App) getLastSwitchover() Switchover {
	var lastSwitch, lastRejectedSwitch Switchover
	err := app.dcs.Get(pathLastSwitch, &lastSwitch)
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error(pathLastSwitch, "error", err)
	}
	errRejected := app.dcs.Get(pathLastRejectedSwitch, &lastRejectedSwitch)
	if errRejected != nil && errRejected != dcs.ErrNotFound {
		app.logger.Error(pathLastRejectedSwitch, "error", errRejected)
	}

	if lastRejectedSwitch.InitiatedAt.After(lastSwitch.InitiatedAt) {
		return lastRejectedSwitch
	}

	return lastSwitch
}

func (app *App) approveSwitchover(switchover *Switchover, activeNodes []string, shardState map[string]*HostState) error {
	if switchover.RunCount > 0 {
		return nil
	}
	permissibleReplicas := countAliveHAReplicasWithinNodes(activeNodes, shardState)
	failoverQuorum := app.getFailoverQuorum(activeNodes)
	if permissibleReplicas < failoverQuorum {
		return fmt.Errorf("no quorum, have %d replicas while %d is required", permissibleReplicas, failoverQuorum)
	}
	return nil
}

func (app *App) startSwitchover(switchover *Switchover) error {
	app.logger.Info(fmt.Sprintf("Switchover: %s => %s starting", switchover.From, switchover.To))
	switchover.StartedAt = time.Now()
	switchover.StartedBy = app.config.Hostname
	return app.dcs.Set(pathCurrentSwitch, switchover)
}

func (app *App) failSwitchover(switchover *Switchover, err error) error {
	app.logger.Error(fmt.Sprintf("Switchover: %s => %s failed", switchover.From, switchover.To), "error", err)
	switchover.RunCount++
	switchover.Progress = nil
	switchover.Result = new(SwitchoverResult)
	switchover.Result.Ok = false
	switchover.Result.Error = err.Error()
	switchover.Result.FinishedAt = time.Now()
	return app.dcs.Set(pathCurrentSwitch, switchover)
}

func (app *App) updateSwitchover(switchover *Switchover) error {
	if switchover.Progress == nil {
		return fmt.Errorf("update switchover without progress is not possible")
	}

	return app.dcs.Set(pathCurrentSwitch, switchover)
}

func (app *App) finishSwitchover(switchover *Switchover, switchErr error) error {
	result := true
	action := "finished"
	path := pathLastSwitch
	if switchErr != nil {
		result = false
		action = "rejected"
		path = pathLastRejectedSwitch
	}

	app.logger.Info(fmt.Sprintf("Switchover: %s => %s %s", switchover.From, switchover.To, action))
	switchover.Progress = nil
	switchover.Result = new(SwitchoverResult)
	switchover.Result.Ok = result
	switchover.Result.FinishedAt = time.Now()

	if switchErr != nil {
		switchover.Result.Error = switchErr.Error()
	}

	err := app.dcs.Delete(pathCurrentSwitch)
	if err != nil {
		return err
	}
	return app.dcs.Set(path, switchover)
}

func filterOut(a, b []string) (res []string) {
	for _, i := range a {
		if !slices.Contains(b, i) {
			res = append(res, i)
		}
	}
	return
}

func (app *App) performSwitchover(shardState map[string]*HostState, activeNodes []string, switchover *Switchover, oldMaster string) error {
	app.enterCritical()
	defer app.exitCritical()
	if switchover.Progress == nil {
		switchover.Progress = new(SwitchoverProgress)
		switchover.Progress.Version = switchoverVersion
		switchover.Progress.Phase = 1
		err := app.updateSwitchover(switchover)
		if err != nil {
			return fmt.Errorf("setting initial switchover progress: %s", err.Error())
		}
	} else if switchover.Progress.Version != switchoverVersion {
		return fmt.Errorf("got incompatible switchover version %d (expected %d)", switchover.Progress.Version,
			switchoverVersion)
	}

	if switchover.To != "" {
		if !slices.Contains(activeNodes, switchover.To) {
			return fmt.Errorf("switchover: failed: replica %s is not active, can't switch to it", switchover.To)
		}
	}

	failoverQuorum := app.getFailoverQuorum(activeNodes)

	if switchover.Cause == CauseAuto && switchover.From == oldMaster {
		activeNodes = filterOut(activeNodes, []string{oldMaster})
	}

	app.logger.Info("Switchover: phase 1: make all shard nodes read-only")

	errsRO := runParallel(func(host string) error {
		if !shardState[host].PingOk {
			err := fmt.Errorf("host %s is not healthy", host)
			app.logger.Error("Setting read-only", "error", err)
			return err
		}
		node := app.shard.Get(host)
		err, rewriteErr := node.SetReadOnly(app.ctx, host == oldMaster)
		if err != nil {
			app.logger.Error(fmt.Sprintf("Setting %s read-only", host), "error", err)
			return err
		}
		if rewriteErr != nil {
			app.logger.Warn(fmt.Sprintf("Unable to rewrite config after making %s read-only", host), "error", rewriteErr)
		}
		app.logger.Info(fmt.Sprintf("Switchover: host %s is read-only", host))
		return nil
	}, activeNodes)

	if err, ok := errsRO[oldMaster]; ok && err != nil && shardState[oldMaster].PingOk {
		err = fmt.Errorf("failed to set old master %s read-only: %s", oldMaster, err.Error())
		app.logger.Error("Switchover", "error", err)
		switchErr := app.finishSwitchover(switchover, err)
		if switchErr != nil {
			return fmt.Errorf("failed to reject switchover %s", switchErr)
		}
		app.logger.Info("Switchover: rejected")
		return err
	}

	poisonPill, err := app.getPoisonPill()
	if err != nil && err != dcs.ErrNotFound {
		return fmt.Errorf("unable to get poison pill: %s", err.Error())
	}
	if !shardState[oldMaster].PingOk {
		needIssue := true
		if poisonPill != nil {
			if poisonPill.TargetHost == oldMaster {
				needIssue = false
			} else {
				err = app.clearPoisonPill()
				if err != nil {
					return fmt.Errorf("unable to clear stale poison pill: %s", err.Error())
				}
				poisonPill = nil
			}
		}
		if needIssue {
			err := app.issuePoisonPill(oldMaster)
			if err != nil {
				return fmt.Errorf("unable to issue poison pill for old master %s: %s", oldMaster, err.Error())
			}
		}
		if switchover.Cause != CauseAuto {
			app.waitPoisonPill(app.config.Redis.WaitPoisonPillTimeout)
		}
	}

	app.logger.Info("Switchover: phase 2: stop replication")

	errsPause := runParallel(func(host string) error {
		if !shardState[host].PingOk {
			err := fmt.Errorf("host %s is not healthy", host)
			app.logger.Error("Pausing replication", "error", err)
			return err
		}
		rs := shardState[host].ReplicaState
		if (rs == nil || !rs.MasterLinkState) && !app.config.Redis.TurnBeforeSwitchover {
			app.logger.Info(fmt.Sprintf("Switchover: skipping replication pause on %s", host))
			return nil
		}
		node := app.shard.Get(host)
		err := node.PauseReplication(app.ctx)
		if err != nil {
			app.logger.Error(fmt.Sprintf("Pausing replication on %s", host), "error", err)
			return err
		}
		app.logger.Info(fmt.Sprintf("Switchover: replication on %s is now paused", host))
		return nil
	}, activeNodes)
	var aliveActiveNodes []string

	for _, host := range activeNodes {
		if errsRO[host] == nil && errsPause[host] == nil {
			aliveActiveNodes = append(aliveActiveNodes, host)
		}
	}

	if len(aliveActiveNodes) < failoverQuorum {
		return fmt.Errorf("no failover quorum reached: %d nodes alive, %d required",
			len(aliveActiveNodes), failoverQuorum)
	}

	app.logger.Info("Switchover: phase 3: find most up-to-date host")

	states, err := app.getShardStateFromDB()
	if err != nil {
		return fmt.Errorf("no actual shard state: %s", err.Error())
	}

	var mostRecent string
	var newMaster string
	if switchover.Progress.Phase >= 3 {
		mostRecent = switchover.Progress.MostRecent
		newMaster = switchover.Progress.NewMaster
	} else {
		mostRecent = app.findMostRecentNode(states)
		if switchover.To != "" {
			newMaster = switchover.To
		} else if switchover.From != "" {
			newMaster, err = app.getMostDesirableNode(states, switchover.From)
			if err != nil {
				errsResume := runParallel(func(host string) error {
					if !shardState[host].PingOk {
						err := fmt.Errorf("host %s is not healthy", host)
						app.logger.Error("Resume replication", "error", err)
						return err
					}
					node := app.shard.Get(host)
					err := node.ResumeReplication(app.ctx)
					if err != nil {
						app.logger.Error(fmt.Sprintf("Resume replication on %s", host), "error", err.Error())
						return err
					}
					app.logger.Info(fmt.Sprintf("Switchover: replication on %s is now resumed", host))
					return nil
				}, activeNodes)
				combined := combineErrors(errsResume)
				if combined != nil {
					app.logger.Error("Resuming replication on desirable host get fail", "error", combined)
				}
				return fmt.Errorf("get desirable node for switchover: %s", err.Error())
			}
		} else {
			newMaster = mostRecent
		}
		switchover.Progress.MostRecent = mostRecent
		switchover.Progress.NewMaster = newMaster
		switchover.Progress.Phase = 3
		err := app.updateSwitchover(switchover)
		if err != nil {
			return fmt.Errorf("setting switchover progress on phase 3: %s", err.Error())
		}
	}

	if switchover.Progress.Phase < 5 {
		app.logger.Info("Switchover: phase 4: catch up")

		if newMaster != mostRecent && getOffset(states[newMaster]) != getOffset(states[mostRecent]) {
			err = app.changeMaster(newMaster, mostRecent)
			if err != nil {
				return err
			}

			err := app.waitForCatchup(newMaster, mostRecent)
			if err != nil {
				return err
			}
		}
	}

	shardState, err = app.getShardStateFromDB()
	if err != nil {
		return fmt.Errorf("update shard state during switchover: %s", err.Error())
	}
	if !shardState[newMaster].PingOk {
		return fmt.Errorf("new master %s suddenly became not available during switchover", newMaster)
	}

	app.logger.Info("Switchover: phase 5: promote selected host")

	if switchover.Progress.Phase != 6 {
		switchover.Progress.Phase = 5
		err := app.updateSwitchover(switchover)
		if err != nil {
			return fmt.Errorf("setting switchover progress on phase 5: %s", err.Error())
		}

		err = app.dcs.Set(pathMasterNode, newMaster)
		if err != nil {
			return fmt.Errorf("failed to set new master in dcs: %s", err)
		}

		poisonPill, err = app.getPoisonPill()
		if err != nil && err != dcs.ErrNotFound {
			return fmt.Errorf("unable to get poison pill: %s", err.Error())
		}
		needIssue := true
		if poisonPill != nil {
			if poisonPill.TargetHost == oldMaster {
				needIssue = false
			} else {
				err = app.clearPoisonPill()
				if err != nil {
					return fmt.Errorf("unable to clear stale poison pill: %s", err.Error())
				}
				poisonPill = nil
			}
		}
		if needIssue {
			err := app.issuePoisonPill(oldMaster)
			if err != nil {
				return fmt.Errorf("unable to issue poison pill for old master %s: %s", oldMaster, err.Error())
			}
		}
		if switchover.Cause != CauseAuto {
			app.waitPoisonPill(app.config.Redis.WaitPoisonPillTimeout)
		}

		if len(aliveActiveNodes) == 1 || app.config.Redis.AllowDataLoss || app.config.Redis.MaxReplicasToWrite == 0 {
			node := app.shard.Get(newMaster)
			err, errConf := node.SetMinReplicas(app.ctx, 0)
			if err != nil {
				return fmt.Errorf("unable to set %s available for write before promote: %s", newMaster, err.Error())
			}
			if errConf != nil {
				return fmt.Errorf("unable to rewrite config on %s before promote: %s", newMaster, errConf.Error())
			}
		}

		if app.config.Redis.TurnBeforeSwitchover {
			var psyncNodes []string
			for _, host := range aliveActiveNodes {
				if host == newMaster {
					continue
				}
				if !shardState[newMaster].IsReplPaused {
					app.logger.Warn(fmt.Sprintf("Unable to psync %s before promote: replication on new master is not paused", host))
					continue
				}
				if isPartialSyncPossible(shardState[host], shardState[newMaster]) {
					psyncNodes = append(psyncNodes, host)
				}
			}

			errs := runParallel(func(host string) error {
				if !shardState[host].PingOk {
					return nil
				}
				err := app.changeMaster(host, newMaster)
				if err != nil {
					return err
				}
				return nil
			}, psyncNodes)

			err := combineErrors(errs)
			if err != nil {
				app.logger.Warn("Unable to psync some replicas before promote", "error", err)
			}
		}
		deadline := time.Now().Add(app.config.Redis.WaitPromoteTimeout)
		forceDeadline := time.Now().Add(app.config.Redis.WaitPromoteForceTimeout)
		promoted := false
		for time.Now().Before(deadline) {
			err = app.promote(newMaster, oldMaster, shardState, forceDeadline)
			if err != nil {
				return fmt.Errorf("promote new master %s failed: %s", newMaster, err.Error())
			}
			time.Sleep(1 * time.Second)
			shardState, err = app.getShardStateFromDB()
			if err != nil {
				return fmt.Errorf("update shard state during switchover after promote: %s", err.Error())
			}
			if shardState[newMaster].IsMaster {
				promoted = true
				break
			}
			app.logger.Warn(fmt.Sprintf("Switchover: phase 5: %s is still replica, trying again", newMaster))
		}
		if !promoted {
			return fmt.Errorf("promote new master %s failed: deadline reached", newMaster)
		}
	}

	switchover.Progress.Phase = 6
	err = app.updateSwitchover(switchover)
	if err != nil {
		return fmt.Errorf("setting switchover progress on phase 6: %s", err.Error())
	}

	app.logger.Info("Switchover: phase 6: turn replicas")

	var psyncNodes []string
	for _, host := range aliveActiveNodes {
		if host == newMaster {
			continue
		}
		if isPartialSyncPossible(shardState[host], shardState[newMaster]) {
			psyncNodes = append(psyncNodes, host)
		}
	}

	psyncActiveNodes := make([]string, len(psyncNodes))
	copy(psyncActiveNodes, psyncNodes)
	psyncActiveNodes = append(psyncActiveNodes, newMaster)

	err = app.dcs.Set(pathActiveNodes, psyncActiveNodes)
	if err != nil {
		return fmt.Errorf("unable to update active nodes in dcs: %s", psyncActiveNodes)
	}

	newMasterNode := app.shard.Get(newMaster)

	app.repairMaster(newMasterNode, psyncActiveNodes, shardState[newMaster])

	errs := runParallel(func(host string) error {
		if host == newMaster || !shardState[host].PingOk {
			return nil
		}
		err := app.changeMaster(host, newMaster)
		if err != nil {
			return err
		}
		return nil
	}, psyncActiveNodes)

	return combineErrors(errs)
}
