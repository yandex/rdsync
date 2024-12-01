package app

import (
	"fmt"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

func countRunningHAReplicas(shardState map[string]*HostState) int {
	cnt := 0
	for _, state := range shardState {
		rs := state.ReplicaState
		if state.PingOk && !state.IsOffline && rs != nil && (rs.MasterLinkState || rs.MasterSyncInProgress) {
			cnt++
		}
	}
	return cnt
}

func (app *App) getFailoverQuorum(activeNodes []string) int {
	fq := len(activeNodes) - app.getNumReplicasToWrite(activeNodes)
	if fq < 1 || app.config.Redis.AllowDataLoss {
		fq = 1
	}
	return fq
}

func (app *App) performFailover(master string) error {
	var switchover Switchover
	switchover.From = master
	switchover.InitiatedBy = app.config.Hostname
	switchover.InitiatedAt = time.Now()
	switchover.Cause = CauseAuto
	return app.dcs.Create(pathCurrentSwitch, switchover)
}

func (app *App) approveFailover(shardState map[string]*HostState, activeNodes []string, master string) error {
	if app.config.Redis.FailoverTimeout > 0 {
		failedTime := time.Since(app.nodeFailTime[master])
		if failedTime < app.config.Redis.FailoverTimeout {
			return fmt.Errorf("failover timeout is not yet elapsed: remaining %v",
				app.config.Redis.FailoverTimeout-failedTime)
		}
	}
	if countRunningHAReplicas(shardState) == len(shardState)-1 {
		return fmt.Errorf("all replicas are alive and running replication, seems dcs problems")
	}

	app.logger.Info(fmt.Sprintf("Approve failover: active nodes are %v", activeNodes))
	permissibleReplicas := countAliveHAReplicasWithinNodes(activeNodes, shardState)
	failoverQuorum := app.getFailoverQuorum(activeNodes)
	if permissibleReplicas < failoverQuorum {
		return fmt.Errorf("no quorum, have %d replicas while %d is required", permissibleReplicas, failoverQuorum)
	}

	var lastSwitchover Switchover
	err := app.dcs.Get(pathLastSwitch, &lastSwitchover)
	if err != dcs.ErrNotFound {
		if err != nil {
			return err
		}
		if lastSwitchover.Result == nil {
			return fmt.Errorf("another switchover with cause %s is in progress", lastSwitchover.Cause)
		}
		timeAfterLastSwitchover := time.Since(lastSwitchover.Result.FinishedAt)
		if timeAfterLastSwitchover < app.config.Redis.FailoverCooldown && lastSwitchover.Cause == CauseAuto {
			return fmt.Errorf("not enough time from last failover %s (cooldown %s)",
				lastSwitchover.Result.FinishedAt, app.config.Redis.FailoverCooldown)
		}
	}
	return nil
}
