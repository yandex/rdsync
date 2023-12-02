package app

import (
	"fmt"
	"time"

	"github.com/yandex/rdsync/internal/redis"
)

func (app *App) updateCache() error {
	var state redis.SentiCacheState
	masterReadOnly := false
	dcsState, err := app.getShardStateFromDcs()
	if err != nil {
		return err
	}
	for fqdn, hostState := range dcsState {
		if hostState == nil || !hostState.PingOk || hostState.Error != "" {
			continue
		}

		if hostState.SentiCacheState != nil && fqdn != app.config.Hostname {
			var sentinel redis.SentiCacheSentinel
			sentinel.Name = hostState.SentiCacheState.Name
			sentinel.RunID = hostState.SentiCacheState.RunID
			if app.config.SentinelMode.AnnounceHostname {
				sentinel.IP = fqdn
			} else {
				sentinel.IP = hostState.IP
			}
			sentinel.Port = app.config.SentinelMode.CachePort
			state.Sentinels = append(state.Sentinels, sentinel)
		}

		if hostState.IsOffline {
			continue
		}

		if hostState.IsMaster {
			if state.Master.IP != "" && !masterReadOnly && !hostState.IsReadOnly {
				return fmt.Errorf("2 open masters: %s and %s", hostState.IP, state.Master.IP)
			}
			if hostState.IsReadOnly && !masterReadOnly {
				continue
			}
			masterReadOnly = hostState.IsReadOnly
			state.Master.Name = app.config.SentinelMode.ClusterName
			state.Master.IP = hostState.IP
			if app.config.SentinelMode.AnnounceHostname {
				state.Master.IP = fqdn
			} else {
				state.Master.IP = hostState.IP
			}
			state.Master.Port = app.config.Redis.Port
			state.Master.RunID = hostState.RunID
			state.Master.Quorum = len(dcsState)/2 + 1
			state.Master.ParallelSyncs = app.config.Redis.MaxParallelSyncs
			state.Master.ConfigEpoch = 0
		} else {
			nc, err := app.shard.GetNodeConfiguration(fqdn)
			if err != nil {
				return err
			}
			var replica redis.SentiCacheReplica
			if app.config.SentinelMode.AnnounceHostname {
				replica.IP = fqdn
			} else {
				replica.IP = hostState.IP
			}
			replica.Port = app.config.Redis.Port
			replica.RunID = hostState.RunID
			replica.MasterLinkDownTime = hostState.ReplicaState.MasterLinkDownTime
			replica.SlavePriority = nc.Priority
			replica.ReplicaAnnounced = 1
			replica.MasterHost = hostState.ReplicaState.MasterHost
			replica.MasterPort = app.config.Redis.Port
			if hostState.ReplicaState.MasterLinkState {
				replica.SlaveMasterLinkStatus = 0
			} else {
				replica.SlaveMasterLinkStatus = 1
			}
			replica.SlaveReplOffset = hostState.ReplicaState.ReplicationOffset
			state.Replicas = append(state.Replicas, replica)
		}
	}
	if state.Master.IP == "" {
		return fmt.Errorf("0 open masters within %d hosts", len(dcsState))
	}
	return app.cache.Update(app.ctx, &state)
}

func (app *App) cacheUpdater() {
	ticker := time.NewTicker(app.config.TickInterval)
	for {
		select {
		case <-ticker.C:
			err := app.updateCache()
			if err != nil {
				app.logger.Error("CacheUpdater: failed to update cache", "error", err)
			}

		case <-app.ctx.Done():
			return
		}
	}
}
