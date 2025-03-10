package app

import (
	"fmt"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) getNumReplicasToWrite(activeNodes []string) int {
	return len(activeNodes) / 2
}

func (app *App) getCurrentMaster(shardState map[string]*HostState) (string, error) {
	var master string
	err := app.dcs.Get(pathMasterNode, &master)
	if err != nil && err != dcs.ErrNotFound {
		return "", fmt.Errorf("failed to get current master from dcs: %s", err)
	}
	if master != "" {
		stateMaster, err := app.getMasterHost(shardState)
		if err != nil {
			app.logger.Warn("Have master in DCS but unable to validate", "error", err)
			return master, nil
		}
		if stateMaster != "" && stateMaster != master {
			app.logger.Warn(fmt.Sprintf("DCS and valkey master state diverged: %s and %s", master, stateMaster))
			allStable := true
			for host, state := range shardState {
				if !state.PingStable || state.IsOffline {
					allStable = false
					app.logger.Warn(fmt.Sprintf("%s is dead skipping divergence fix", host))
					break
				}
			}
			if allStable {
				return app.ensureCurrentMaster(shardState)
			}
		}
		return master, nil
	}
	return app.ensureCurrentMaster(shardState)
}

func (app *App) getMasterHost(shardState map[string]*HostState) (string, error) {
	masters := make([]string, 0)
	for host, state := range shardState {
		if state.PingOk && state.IsMaster {
			masters = append(masters, host)
		}
	}
	if len(masters) > 1 {
		if app.mode == modeCluster {
			mastersWithSlots := make([]string, 0)
			for _, master := range masters {
				node := app.shard.Get(master)
				hasSlots, err := node.HasClusterSlots(app.ctx)
				if err != nil {
					return "", fmt.Errorf("unable to check slots on %s", master)
				}
				if hasSlots {
					mastersWithSlots = append(mastersWithSlots, master)
				}
			}
			if len(mastersWithSlots) == 1 {
				return mastersWithSlots[0], nil
			}
		}
		return "", fmt.Errorf("got more than 1 master: %s", masters)
	}
	if len(masters) == 0 {
		return "", nil
	}
	return masters[0], nil
}

func (app *App) ensureCurrentMaster(shardState map[string]*HostState) (string, error) {
	master, err := app.getMasterHost(shardState)
	if err != nil {
		return "", err
	}
	if master == "" {
		return "", fmt.Errorf("no master in shard of %d nodes", len(shardState))
	}
	err = app.dcs.Set(pathMasterNode, master)
	if err != nil {
		return "", fmt.Errorf("failed to set current master in dcs: %s", err)
	}
	return master, nil
}

func (app *App) changeMaster(host, master string) error {
	if host == master {
		return fmt.Errorf("changing %s replication source to itself", host)
	}

	node := app.shard.Get(host)
	masterState := app.getHostState(master)
	masterNode := app.shard.Get(master)
	state := app.getHostState(host)

	if !state.PingOk {
		return fmt.Errorf("changeMaster: replica %s is dead - unable to init repair", host)
	}

	app.repairReplica(node, masterState, state, master, host)

	deadline := time.Now().Add(app.config.Valkey.WaitReplicationTimeout)
	for time.Now().Before(deadline) {
		state = app.getHostState(host)
		rs := state.ReplicaState
		if rs != nil && replicates(masterState, rs, host, masterNode, false) {
			break
		}
		if !state.PingOk {
			return fmt.Errorf("changeMaster: replica %s died while waiting to start replication from %s", host, master)
		}
		masterState = app.getHostState(master)
		if !masterState.PingOk {
			return fmt.Errorf("changeMaster: %s died while waiting to start replication to %s", master, host)
		}
		app.logger.Info(fmt.Sprintf("ChangeMaster: waiting for %s to start replication from %s", host, master))
		app.repairReplica(node, masterState, state, master, host)
		time.Sleep(time.Second)
	}
	rs := state.ReplicaState
	if rs != nil && replicates(masterState, rs, host, masterNode, false) {
		app.logger.Info(fmt.Sprintf("ChangeMaster: %s started replication from %s", host, master))
	} else {
		return fmt.Errorf("%s was unable to start replication from %s", host, master)
	}
	return nil
}

func (app *App) waitForCatchup(host, master string) error {
	if host == master {
		return fmt.Errorf("waiting for %s to catchup with itself", host)
	}

	deadline := time.Now().Add(app.config.Valkey.WaitCatchupTimeout)
	for time.Now().Before(deadline) {
		masterState := app.getHostState(master)
		if !masterState.PingOk {
			return fmt.Errorf("waitForCatchup: %s died while waiting for catchup on %s", master, host)
		}
		state := app.getHostState(host)
		if !state.PingOk {
			return fmt.Errorf("waitForCatchup: replica %s died while waiting for catchup from %s", host, master)
		}
		if state.ReplicaState == nil {
			app.logger.Warn(fmt.Sprintf("WaitForCatchup: %s has invalid replica state", host))
			time.Sleep(time.Second)
			continue
		}
		var masterOffset int64
		if masterState.IsMaster {
			masterOffset = masterState.MasterReplicationOffset
		} else if masterState.ReplicaState == nil {
			app.logger.Warn(fmt.Sprintf("WaitForCatchup: %s has invalid replica state", master))
			time.Sleep(time.Second)
			continue
		} else {
			masterOffset = masterState.ReplicaState.ReplicationOffset
		}
		if masterOffset <= state.ReplicaState.ReplicationOffset {
			return nil
		}
		app.logger.Info(fmt.Sprintf("WaitForCatchup: waiting for %s (offset=%d) to catchup with %s (offset=%d)", host, state.ReplicaState.ReplicationOffset, master, masterOffset))
		time.Sleep(time.Second)
	}

	return fmt.Errorf("timeout waiting for %s to catchup with %s", host, master)
}

func (app *App) promote(master, oldMaster string, shardState map[string]*HostState, forceDeadline time.Time) error {
	node := app.shard.Get(master)

	if shardState[master].IsMaster {
		app.logger.Info(fmt.Sprintf("%s is already master", master))
		return nil
	}

	switch app.mode {
	case modeSentinel:
		return node.SentinelPromote(app.ctx)
	case modeCluster:
		if shardState[oldMaster].PingOk {
			if time.Now().Before(forceDeadline) {
				app.logger.Info("Old master alive. Using FORCE to promote")
				return node.ClusterPromoteForce(app.ctx)
			}
		}
		majorityAlive, err := node.IsClusterMajorityAlive(app.ctx)
		if err != nil {
			app.logger.Error("New master is not able to check cluster majority state. Assuming that majority is alive.", "error", err)
			majorityAlive = true
		}
		if majorityAlive {
			app.logger.Info("Majority of master nodes in cluster alive. Using FORCE to promote")
			return node.ClusterPromoteForce(app.ctx)
		}
		app.logger.Info("Old master is dead and majority of master nodes in cluster dead. Using TAKEOVER to promote")
		return node.ClusterPromoteTakeover(app.ctx)
	}

	return fmt.Errorf("running promote with unsupported mode: %s", app.mode)
}
