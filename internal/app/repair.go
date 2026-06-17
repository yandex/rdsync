package app

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/yandex/rdsync/internal/valkey"
)

func (app *App) repairShard(shardState map[string]*HostState, activeNodes []string, master string) {
	replicas := make([]string, 0)
	syncing := 0
	masterState := shardState[master]
	masterNode := app.shard.Get(master)
	for host, state := range shardState {
		if !state.PingOk {
			continue
		}
		if host == master {
			app.repairMaster(masterNode, activeNodes, state)
		} else {
			rs := state.ReplicaState
			if rs != nil && rs.MasterSyncInProgress && replicates(masterState, rs, host, masterNode, true) {
				syncing++
			}
			replicas = append(replicas, host)
		}
	}
	for _, host := range replicas {
		state := shardState[host]
		node := app.shard.Get(host)
		if !state.IsReadOnly {
			err, rewriteErr := node.SetReadOnly(app.ctx, false)
			if err != nil {
				app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Unable to make replica read-only")
			}
			if rewriteErr != nil {
				app.logger.Error().Str("fqdn", node.FQDN()).Err(rewriteErr).Msg("Unable to rewrite config after making replica read-only")
			}
		}
		rs := state.ReplicaState
		if rs == nil || state.IsReplPaused || !replicates(masterState, rs, host, masterNode, true) {
			if syncing < app.config.Valkey.MaxParallelSyncs {
				app.repairReplica(node, masterState, state, master, host)
				syncing++
			} else {
				app.logger.Error().Msgf("Leaving replica %s broken: currently syncing %d/%d", host, syncing, app.config.Valkey.MaxParallelSyncs)
			}
		}
	}
}

func (app *App) repairMaster(node *valkey.Node, activeNodes []string, state *HostState) {
	expectedNumReplicas := app.getNumReplicasToWrite(activeNodes)
	actualNumReplicas, err := node.GetNumQuorumReplicas(app.ctx)
	if err != nil {
		app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Unable to get actual num quorum replicas on master")
		return
	}
	if expectedNumReplicas > actualNumReplicas {
		app.logger.Info().Str("fqdn", node.FQDN()).Msgf("Changing num quorum replicas from %d to %d on master", actualNumReplicas, expectedNumReplicas)
		err, rewriteErr := node.SetNumQuorumReplicas(app.ctx, expectedNumReplicas)
		if err != nil {
			app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Unable to set num quorum replicas on master")
			return
		}
		if rewriteErr != nil {
			app.logger.Error().Str("fqdn", node.FQDN()).Err(rewriteErr).Msg("Unable to rewrite config on master")
		}
	}
	if state.IsReadOnly || state.MinReplicasToWrite != 0 {
		err, rewriteErr := node.SetReadWrite(app.ctx)
		if err != nil {
			app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Unable to set master read-write")
		}
		if rewriteErr != nil {
			app.logger.Error().Str("fqdn", node.FQDN()).Err(rewriteErr).Msg("Unable to rewrite config on master")
		}
	}
	if state.IsReplPaused {
		err := node.ResumeReplication(app.ctx)
		if err != nil {
			app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Unable to make resume replication on master")
		}
	}
}

func (app *App) repairReplica(node *valkey.Node, masterState, state *HostState, master, replicaFQDN string) {
	masterNode := app.shard.Get(master)
	rs := state.ReplicaState
	if node.IsLocal() {
		if app.replFailTime.IsZero() {
			app.replFailTime = time.Now()
		}
		if time.Since(app.replFailTime) > app.config.Valkey.DestructiveReplicationRepairTimeout && app.config.Valkey.DestructiveReplicationRepairCommand != "" {
			app.logger.Error().Msgf("Replication is broken for too long: %s. Using destructive repair: %s",
				time.Since(app.replFailTime), app.config.Valkey.DestructiveReplicationRepairCommand)
			split := strings.Fields(app.config.Valkey.DestructiveReplicationRepairCommand)
			cmd := exec.CommandContext(app.ctx, split[0], split[1:]...)
			err := cmd.Run()
			if err != nil {
				app.logger.Error().Err(err).Msg("Unable to run destructive replication repair on local node")
			} else {
				app.replFailTime = time.Now()
			}
		}
	}
	if !replicates(masterState, rs, replicaFQDN, masterNode, true) {
		app.logger.Info().Str("fqdn", replicaFQDN).Msg("Initiating replica repair")
		switch app.mode {
		case modeSentinel:
			err := node.SentinelMakeReplica(app.ctx, master)
			if err != nil {
				app.logger.Error().Err(err).Msgf("Unable to make %s replica of %s", node.FQDN(), master)
			}
		case modeCluster:
			alone, err := node.IsClusterNodeAlone(app.ctx)
			if err != nil {
				app.logger.Error().Err(err).Msgf("Unable to check if %s is alone", node.FQDN())
				return
			}
			if alone {
				masterIP, err := masterNode.GetIP()
				if err != nil {
					app.logger.Error().Err(err).Msgf("Unable to make %s replica of %s", node.FQDN(), master)
					return
				}
				err = node.ClusterMeet(app.ctx, masterIP, app.config.Valkey.Port, app.config.Valkey.ClusterBusPort)
				if err != nil {
					app.logger.Error().Err(err).Msgf("Unable to make %s meet with master %s at %s:%d:%d", node.FQDN(), master, masterIP, app.config.Valkey.Port, app.config.Valkey.ClusterBusPort)
					return
				}
			}
			masterID, err := masterNode.ClusterGetID(app.ctx)
			if err != nil {
				app.logger.Error().Err(err).Msgf("Unable to get cluster id of %s", master)
				return
			}
			err = node.ClusterMakeReplica(app.ctx, masterID)
			if err != nil {
				app.logger.Error().Err(err).Msgf("Unable to make %s replica of %s (%s)", node.FQDN(), master, masterID)
			}
		}
	}
	if state.IsReplPaused {
		err := node.ResumeReplication(app.ctx)
		if err != nil {
			app.logger.Error().Str("fqdn", node.FQDN()).Err(err).Msg("Unable to resume replication")
		}
	}
}

func (app *App) reservedConnectionsWatchdog(info map[string]string) error {
	maxClients, ok := info["maxclients"]
	if !ok {
		return fmt.Errorf("no maxclients in info")
	}
	parsedMaxClients, err := strconv.ParseInt(maxClients, 10, 64)
	if err != nil {
		return fmt.Errorf("unable to parse maxclients from info: %w", err)
	}
	clusterConns, ok := info["cluster_connections"]
	if !ok {
		return fmt.Errorf("no cluster_connections in info")
	}
	parsedClusterConns, err := strconv.ParseInt(clusterConns, 10, 64)
	if err != nil {
		return fmt.Errorf("unable to parse cluster_connections from info: %w", err)
	}
	connectedCliends, ok := info["connected_clients"]
	if !ok {
		return fmt.Errorf("no connected_clients in info")
	}
	parsedConnectedClients, err := strconv.ParseInt(connectedCliends, 10, 64)
	if err != nil {
		return fmt.Errorf("unable to parse connected_clients from info: %w", err)
	}
	freeConns := parsedMaxClients - parsedClusterConns - parsedConnectedClients
	if freeConns < int64(app.config.Valkey.ReservedConnections) {
		app.logger.Warn().Msgf("Local node has %d free connections left. Killing all client connections.", freeConns)
		node := app.shard.Local()
		err = node.DisconnectClients(app.ctx, "normal")
		if err != nil {
			return err
		}
		return node.DisconnectClients(app.ctx, "pubsub")
	}
	return nil
}

func (app *App) repairLocalNode(master string) bool {
	local := app.shard.Local()

	info, _, _, offline, replPaused, err := local.GetState(app.ctx)
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to get local node offline state")
		if app.nodeFailTime[local.FQDN()].IsZero() {
			app.nodeFailTime[local.FQDN()] = time.Now()
		}
		failedTime := time.Since(app.nodeFailTime[local.FQDN()])
		if failedTime > app.config.Valkey.BusyTimeout && strings.HasPrefix(err.Error(), "BUSY ") {
			err = local.ScriptKill(app.ctx)
			if err != nil {
				app.logger.Error().Err(err).Msg("Local node is busy running a script. But SCRIPT KILL failed")
			}
		}
		if strings.HasPrefix(err.Error(), "LOADING ") {
			app.nodeFailTime[local.FQDN()] = time.Now()
		} else if failedTime > app.config.Valkey.RestartTimeout {
			app.nodeFailTime[local.FQDN()] = time.Now()
			err = local.Restart(app.ctx)
			if err != nil {
				app.logger.Error().Err(err).Msg("Unable to restart local node")
			}
		}
	} else if !offline {
		// Report node_offline duration when node comes back online naturally
		if failTime, ok := app.nodeFailTime[local.FQDN()]; ok {
			dur := time.Since(failTime)
			app.timings.reportTiming("node_offline", dur)
		}
		delete(app.nodeFailTime, local.FQDN())
	}

	if !offline {
		err = app.adjustAofMode(master)
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to adjust aof config on local node")
		}
		err = app.closeStaleReplica(master)
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to close local node on staleness")
		}
		err = app.reservedConnectionsWatchdog(info)
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to run reserved connections watchdog")
		}
		return true
	}

	shardState, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error().Err(err).Msg("Local repair: unable to get actual shard state")
		return false
	}
	state, ok := shardState[local.FQDN()]
	if !ok {
		app.logger.Error().Msg("Local repair: unable to find local node in shard state")
		return true
	}
	if master == local.FQDN() && len(shardState) != 1 {
		activeNodes, err := app.GetActiveNodes()
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to get active nodes for local node repair")
			return true
		}
		activeSet := make(map[string]struct{}, len(activeNodes))
		for _, node := range activeNodes {
			activeSet[node] = struct{}{}
		}
		aheadHosts := 0
		baseOffset := getOffset(state)
		for host, hostState := range shardState {
			if host == master {
				continue
			}
			if baseOffset < getOffset(hostState) {
				if _, ok := activeSet[host]; ok {
					app.logger.Warn().Msgf("Host %s is ahead in replication history", host)
					aheadHosts++
				}
			}
			if aheadHosts != 0 {
				app.logger.Error().Msgf("Not making local node online: %d nodes are ahead in replication history", aheadHosts)
				return false
			}
		}
	} else if master == local.FQDN() {
		if !state.IsMaster {
			app.logger.Error().Msg("Local node is alone in shard and is replica. Promoting")
			if err := app.promote(master, master, shardState, time.Now().Add(app.config.Valkey.WaitPromoteForceTimeout)); err != nil {
				app.logger.Error().Err(err).Msg("Unable to promote lone node in shard")
				return false
			}
		}
	} else if app.isReplicaStale(state.ReplicaState, true) {
		shardState, err := app.getShardStateFromDcs()
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to get shard state from dcs on slate replica open")
		}
		syncing := 0
		for host, hostState := range shardState {
			if !hostState.PingOk {
				continue
			}
			if host != master {
				rs := hostState.ReplicaState
				if rs != nil && rs.MasterSyncInProgress {
					syncing++
				}
			}
		}

		if replPaused || !replicates(shardState[master], state.ReplicaState, local.FQDN(), nil, true) {
			if syncing < app.config.Valkey.MaxParallelSyncs {
				app.logger.Info().Msg("Repairing local replica as it is offline and not replicates from primary")
				app.repairReplica(local, shardState[master], state, master, local.FQDN())
			} else {
				app.logger.Error().Msgf("Leaving local offline replica broken: currently syncing %d/%d", syncing, app.config.Valkey.MaxParallelSyncs)
			}
		}
		if shardState[master].PingOk && shardState[master].PingStable && time.Since(shardState[master].CheckAt) < 3*app.config.HealthCheckInterval {
			app.logger.Error().Msg("Not making local node online: considered stale")
			return false
		}
	}
	// Report node_offline duration when node is brought back online after repair
	if failTime, ok := app.nodeFailTime[local.FQDN()]; ok {
		dur := time.Since(failTime)
		app.timings.reportTiming("node_offline", dur)
		delete(app.nodeFailTime, local.FQDN())
	}
	if master == local.FQDN() {
		activeNodes, err := app.GetActiveNodes()
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to get active nodes before setting local master online")
			return false
		}
		expectedNumReplicas := app.getNumReplicasToWrite(activeNodes)
		actualNumReplicas, err := local.GetNumQuorumReplicas(app.ctx)
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to get num quorum replicas before setting local master online")
			return false
		}
		if expectedNumReplicas > actualNumReplicas {
			app.logger.Info().Msgf("Setting num quorum replicas to %d before setting local master online", expectedNumReplicas)
			err, rewriteErr := local.SetNumQuorumReplicas(app.ctx, expectedNumReplicas)
			if err != nil {
				app.logger.Error().Err(err).Msg("Unable to set num quorum replicas before setting local master online")
				return false
			}
			if rewriteErr != nil {
				app.logger.Error().Err(rewriteErr).Msg("Unable to rewrite config after setting num quorum replicas on local master")
			}
		}
	}
	err = local.SetOnline(app.ctx)
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to set local node online")
		return false
	}
	if !app.dcsDivergeTime.IsZero() {
		app.logger.Info().Msg("Clearing DCS divergence time state")
		app.dcsDivergeTime = time.Time{}
	}
	return true
}
