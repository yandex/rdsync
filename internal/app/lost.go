package app

import (
	"fmt"
)

func (app *App) stateLost() appState {
	if app.dcs.IsConnected() {
		return stateCandidate
	}
	if len(app.shard.Hosts()) == 1 {
		return stateLost
	}

	localNodeState := app.getLocalState()
	node := app.shard.Local()
	if localNodeState.IsMaster {
		if app.checkHAReplicasRunning() {
			offline, err := node.IsOffline(app.ctx)
			if err != nil {
				app.logger.Error("Failed to get node offline state", "fqdn", node.FQDN(), "error", err)
				return stateLost
			}
			if offline {
				app.logger.Info("Rdsync have lost connection to ZK. However HA cluster is alive. Setting local node online")
				err = node.SetOnline(app.ctx)
				if err != nil {
					app.logger.Error("Unable to set local node online", "error", err)
				}
				return stateLost
			}
			app.logger.Info("Rdsync have lost connection to ZK. However HA cluster is alive. Do nothing")
			return stateLost
		}
	} else {
		shardState, err := app.getShardStateFromDB()
		if err != nil {
			app.logger.Error("Failed to get shard state from DB", "error", err)
			return stateLost
		}

		app.logger.Info(fmt.Sprintf("Shard state: %v", shardState))
		master, err := app.getMasterHost(shardState)
		if err != nil || master == "" {
			app.logger.Error("Failed to get master from shard state", "error", err)
		} else {
			local := app.shard.Local()
			offline, err := local.IsOffline(app.ctx)
			if err != nil {
				app.logger.Error("Failed to get node offline state", "fqdn", local.FQDN(), "error", err)
				return stateLost
			}
			if shardState[master].PingOk && shardState[master].PingStable && replicates(shardState[master], shardState[local.FQDN()].ReplicaState, local.FQDN(), app.shard.Get(master), false) {
				if offline {
					app.logger.Info("Rdsync have lost connection to ZK. However our replication connection is alive. Setting local node online")
					err = node.SetOnline(app.ctx)
					if err != nil {
						app.logger.Error("Unable to set local node online", "error", err)
					}
					return stateLost
				}
				app.logger.Info("Rdsync have lost connection to ZK. However our replication connection is alive. Do nothing")
				return stateLost
			}
		}
	}

	offline, err := node.IsOffline(app.ctx)
	if err != nil {
		app.logger.Error("Failed to get node offline state", "fqdn", node.FQDN(), "error", err)
		return stateLost
	}
	if offline {
		return stateLost
	}
	if err := node.SetOffline(app.ctx); err != nil {
		app.logger.Error("Failed to set node offline", "fqdn", node.FQDN(), "error", err)
		return stateLost
	}
	app.logger.Info("Rdsync have lost connection to ZK. Node is now offline", "fqdn", node.FQDN())
	return stateLost
}
