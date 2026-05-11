package app

import (
	"time"
)

func (app *App) stateLost() appState {
	if app.dcs.IsConnected() {
		return stateCandidate
	}
	if !app.lostSince.IsZero() && time.Since(app.lostSince) >= app.config.DcsReconnectTimeout {
		app.logger.Warn().Msgf("Lost state persisted for %s, attempting DCS reconnection", time.Since(app.lostSince).Truncate(time.Second))
		err := app.reconnectDCS()
		app.lostSince = time.Now()
		if err != nil {
			app.logger.Error().Err(err).Msg("DCS reconnection attempt failed, will retry later")
		}
		return stateLost
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
				app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Failed to get node offline state")
				return stateLost
			}
			if offline {
				app.logger.Info().Msg("Rdsync have lost connection to ZK. However HA cluster is alive. Setting local node online")
				err = node.SetOnline(app.ctx)
				if err != nil {
					app.logger.Error().Err(err).Msg("Unable to set local node online")
				}
				return stateLost
			}
			app.logger.Info().Msg("Rdsync have lost connection to ZK. However HA cluster is alive. Do nothing")
			return stateLost
		}
	} else {
		shardState, err := app.getShardStateFromDB()
		if err != nil {
			app.logger.Error().Err(err).Msg("Failed to get shard state from DB")
			return stateLost
		}

		app.logger.Info().Msgf("Shard state: %v", shardState)
		master, err := app.getMasterHost(shardState)
		if err != nil || master == "" {
			app.logger.Error().Err(err).Msg("Failed to get master from shard state")
		} else {
			local := app.shard.Local()
			offline, err := local.IsOffline(app.ctx)
			if err != nil {
				app.logger.Error().Str("fqdn", local.FQDN()).Err(err).Msg("Failed to get node offline state")
				return stateLost
			}
			if shardState[master].PingOk && shardState[master].PingStable && replicates(shardState[master], shardState[local.FQDN()].ReplicaState, local.FQDN(), app.shard.Get(master), false) && !app.isReplicaStale(shardState[local.FQDN()].ReplicaState, false) {
				if offline {
					app.logger.Info().Msg("Rdsync have lost connection to ZK. However our replication connection is alive. Setting local node online")
					err = node.SetOnline(app.ctx)
					if err != nil {
						app.logger.Error().Err(err).Msg("Unable to set local node online")
					}
					return stateLost
				}
				app.logger.Info().Msg("Rdsync have lost connection to ZK. However our replication connection is alive. Do nothing")
				return stateLost
			}
		}
	}

	offline, err := node.IsOffline(app.ctx)
	if err != nil {
		app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Failed to get node offline state")
		return stateLost
	}
	if offline {
		return stateLost
	}
	if err := node.SetOffline(app.ctx); err != nil {
		app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Failed to set node offline")
		return stateLost
	}
	app.logger.Info().Str("fqdn", node.FQDN()).Msg("Rdsync have lost connection to ZK. Node is now offline")
	return stateLost
}
