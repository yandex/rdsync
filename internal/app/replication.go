package app

import (
	"fmt"
	"slices"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
	"github.com/yandex/rdsync/internal/valkey"
)

func replicates(masterState *HostState, replicaState *ReplicaState, replicaFQDN string, masterNode *valkey.Node, allowSync bool) bool {
	if replicaState == nil || (!replicaState.MasterLinkState && !allowSync) {
		return false
	}
	if masterState != nil && slices.Contains(masterState.ConnectedReplicas, replicaFQDN) {
		return true
	}
	return masterNode != nil && masterNode.MatchHost(replicaState.MasterHost)
}

func (app *App) isReplicaStale(replicaState *ReplicaState, checkOpenLag bool) bool {
	targetLag := app.config.Valkey.StaleReplicaLagClose
	if checkOpenLag {
		targetLag = app.config.Valkey.StaleReplicaLagOpen
	}
	if replicaState == nil {
		if app.dcsDivergeTime.IsZero() {
			app.dcsDivergeTime = time.Now()
		}
		result := time.Since(app.dcsDivergeTime) > targetLag
		if !result {
			app.logger.Info(fmt.Sprintf("Local node is primary and we got a dcs info divergence at %v. Waiting for %v to make decision.", app.dcsDivergeTime, time.Since(app.dcsDivergeTime)-targetLag))
		} else {
			app.logger.Info(fmt.Sprintf("Local node is primary and we got a dcs info divergence at %v. Marking local node as stale.", app.dcsDivergeTime))
		}
		return result
	}
	if !replicaState.MasterLinkState {
		if replicaState.MasterSyncInProgress || checkOpenLag {
			return true
		}
		return replicaState.MasterLinkDownTime < 0 || time.Duration(replicaState.MasterLinkDownTime)*time.Millisecond > targetLag
	} else {
		return replicaState.MasterLastIOSeconds < 0 || time.Duration(replicaState.MasterLastIOSeconds)*time.Second > targetLag
	}
}

func (app *App) closeStaleReplica(master string) error {
	local := app.shard.Local()
	if local.FQDN() == master {
		if !app.dcsDivergeTime.IsZero() {
			app.logger.Info("Clearing DCS divergence time state")
			app.dcsDivergeTime = time.Time{}
		}
		return nil
	}
	if app.mode == modeCluster {
		hasSlots, err := local.HasClusterSlots(app.ctx)
		if err != nil {
			return err
		}
		if hasSlots {
			return nil
		}
	}
	paused, err := local.IsReplPaused(app.ctx)
	if err != nil {
		return err
	}
	if paused {
		return nil
	}
	localState := app.getHostState(local.FQDN())
	if app.isReplicaStale(localState.ReplicaState, false) {
		app.logger.Debug("Local node seems stale. Checking if we could close.")
		var switchover Switchover
		err := app.dcs.Get(pathCurrentSwitch, &switchover)
		if err == nil {
			app.logger.Debug(fmt.Sprintf("Skipping staleness close due to switchover in progress: %v.", switchover))
			return nil
		}
		if err != dcs.ErrNotFound {
			return err
		}
		shardState, err := app.getShardStateFromDcs()
		if err != nil {
			return err
		}
		if _, ok := shardState[master]; !ok {
			return fmt.Errorf("no %s in shard state from dcs: %+v", master, shardState)
		}
		if shardState[master].PingOk && shardState[master].PingStable && time.Since(shardState[master].CheckAt) < 3*app.config.HealthCheckInterval {
			okReplicas := 0
			staleReplicas := 0
			for host, state := range shardState {
				if host == master {
					continue
				}
				if !state.IsReplPaused && app.isReplicaStale(state.ReplicaState, false) {
					staleReplicas++
				} else if host != local.FQDN() {
					okReplicas++
				}
			}
			if okReplicas >= staleReplicas {
				offline, err := local.IsOffline(app.ctx)
				if err != nil {
					return err
				}
				if offline {
					return nil
				}
				app.logger.Error(fmt.Sprintf("Local node is stale. Alive replicas: %d, stale replicas: %d. Making local node offline.", okReplicas, staleReplicas))
				return local.SetOffline(app.ctx)
			}
		}
	} else if !app.replFailTime.IsZero() {
		app.logger.Debug("Clearing local node replication fail time")
		app.replFailTime = time.Time{}
	}
	return nil
}
