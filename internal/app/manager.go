package app

import (
	"fmt"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) stateManager() appState {
	if !app.dcs.IsConnected() {
		return stateLost
	}
	if !app.dcs.AcquireLock(pathManagerLock) {
		return stateCandidate
	}

	err := app.shard.UpdateHostsInfo()
	if err != nil {
		app.logger.Error("Updating hosts info failed", "error", err)
	}

	shardState, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error("Failed to get shard state from DB", "error", err)
		return stateManager
	}

	shardStateDcs, err := app.getShardStateFromDcs()
	if err != nil {
		app.logger.Error("Failed to get shard state from DCS", "error", err)
		return stateManager
	}

	master, err := app.getCurrentMaster(shardState)
	if err != nil {
		app.logger.Error("Failed to get or identify master", "error", err)
		return stateManager
	}

	activeNodes, err := app.GetActiveNodes()
	if err != nil {
		app.logger.Error("Failed to get active nodes", "error", err)
		return stateManager
	}
	app.logger.Info(fmt.Sprintf("Active nodes: %v", activeNodes))
	app.logger.Info(fmt.Sprintf("Master: %s", master))
	app.logger.Info(fmt.Sprintf("Shard state: %v", shardState))
	app.logger.Info(fmt.Sprintf("DCS shard state: %v", shardStateDcs))

	maintenance, err := app.GetMaintenance()
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error("Failed to get maintenance from dcs", "error", err)
		return stateManager
	}
	if maintenance != nil {
		if !maintenance.RdSyncPaused {
			app.logger.Info("Entering maintenance")
			err := app.enterMaintenance(maintenance, master)
			if err != nil {
				app.logger.Error("Unable to enter maintenance", "error", err)
				return stateManager
			}
		}
		return stateMaintenance
	}

	updateActive := app.repairLocalNode(master)

	var switchover Switchover
	if err := app.dcs.Get(pathCurrentSwitch, &switchover); err == nil {
		err = app.approveSwitchover(&switchover, activeNodes, shardState)
		if err != nil {
			app.logger.Error("Unable to perform switchover", "error", err)
			err = app.finishSwitchover(&switchover, err)
			if err != nil {
				app.logger.Error("Failed to reject switchover", "error", err)
			}
			return stateManager
		}

		err = app.startSwitchover(&switchover)
		if err != nil {
			app.logger.Error("Unable to start switchover", "error", err)
			return stateManager
		}
		err = app.performSwitchover(shardState, activeNodes, &switchover, master)
		if app.dcs.Get(pathCurrentSwitch, new(Switchover)) == dcs.ErrNotFound {
			app.logger.Error("Switchover was aborted")
		} else {
			if err != nil {
				err = app.failSwitchover(&switchover, err)
				if err != nil {
					app.logger.Error("Failed to report switchover failure", "error", err)
				}
			} else {
				err = app.finishSwitchover(&switchover, nil)
				if err != nil {
					app.logger.Error("Failed to report switchover finish", "error", err)
				}
			}
		}
		return stateManager
	} else if err != dcs.ErrNotFound {
		app.logger.Error("Getting current switchover failed", "error", err)
		return stateManager
	}
	poisonPill, err := app.getPoisonPill()
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error("Manager: failed to get poison pill from DCS", "error", err)
		return stateManager
	}
	if poisonPill != nil {
		err = app.clearPoisonPill()
		if err != nil {
			app.logger.Error("Manager: failed to remove poison pill from DCS", "error", err)
			return stateManager
		}
	}
	if (!shardStateDcs[master].PingOk && !shardState[master].PingOk) || shardStateDcs[master].IsOffline {
		app.logger.Error(fmt.Sprintf("Master %s failure", master))
		if app.nodeFailTime[master].IsZero() {
			app.nodeFailTime[master] = time.Now()
		}
		err = app.approveFailover(shardState, activeNodes, master)
		if err == nil {
			app.logger.Info("Failover approved")
			err = app.performFailover(master)
			if err != nil {
				app.logger.Error("Unable to perform failover", "error", err)
			}
		} else {
			app.logger.Error("Failover was not approved", "error", err)
		}
		return stateManager
	}
	if !shardState[master].PingOk {
		app.logger.Error(fmt.Sprintf("Master %s probably failed, do not perform any kind of repair", master))
		return stateManager
	}
	delete(app.nodeFailTime, master)
	app.repairShard(shardState, activeNodes, master)

	if updateActive {
		err = app.updateActiveNodes(shardState, shardStateDcs, activeNodes, master)
		if err != nil {
			app.logger.Error("Failed to update active nodes in dcs", "error", err)
		}
	}

	return stateManager
}
