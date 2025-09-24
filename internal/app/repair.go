package app

import (
	"fmt"
	"log/slog"
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
				app.logger.Error("Unable to make replica read-only", slog.String("fqdn", node.FQDN()), slog.Any("error", err))
			}
			if rewriteErr != nil {
				app.logger.Error("Unable to rewrite config after making replica read-only", slog.String("fqdn", node.FQDN()), slog.Any("error", rewriteErr))
			}
		}
		rs := state.ReplicaState
		if rs == nil || state.IsReplPaused || !replicates(masterState, rs, host, masterNode, true) {
			if syncing < app.config.Valkey.MaxParallelSyncs {
				app.repairReplica(node, masterState, state, master, host)
				syncing++
			} else {
				app.logger.Error(fmt.Sprintf("Leaving replica %s broken: currently syncing %d/%d", host, syncing, app.config.Valkey.MaxParallelSyncs))
			}
		}
	}
}

func (app *App) repairMaster(node *valkey.Node, activeNodes []string, state *HostState) {
	if state.IsReadOnly || state.MinReplicasToWrite != 0 {
		err, rewriteErr := node.SetReadWrite(app.ctx)
		if err != nil {
			app.logger.Error("Unable to set master read-write", slog.String("fqdn", node.FQDN()), slog.Any("error", err))
		}
		if rewriteErr != nil {
			app.logger.Error("Unable to rewrite config on master", slog.String("fqdn", node.FQDN()), slog.Any("error", rewriteErr))
		}
	}
	expectedNumReplicas := app.getNumReplicasToWrite(activeNodes)
	actualNumReplicas, err := node.GetNumQuorumReplicas(app.ctx)
	if err != nil {
		app.logger.Error("Unable to get actual num quorum replicas on master", slog.String("fqdn", node.FQDN()), slog.Any("error", err))
		return
	}
	if actualNumReplicas != expectedNumReplicas {
		app.logger.Info(fmt.Sprintf("Changing num quorum replicas from %d to %d on master", actualNumReplicas, expectedNumReplicas), slog.String("fqdn", node.FQDN()))
		err, rewriteErr := node.SetNumQuorumReplicas(app.ctx, expectedNumReplicas)
		if err != nil {
			app.logger.Error("Unable to set num quorum replicas on master", slog.String("fqdn", node.FQDN()), slog.Any("error", err))
		}
		if rewriteErr != nil {
			app.logger.Error("Unable to rewrite config on master", slog.String("fqdn", node.FQDN()), slog.Any("error", rewriteErr))
		}
	}
	if state.IsReplPaused {
		err := node.ResumeReplication(app.ctx)
		if err != nil {
			app.logger.Error("Unable to make resume replication on master", slog.String("fqdn", node.FQDN()), slog.Any("error", err))
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
			app.logger.Error(fmt.Sprintf("Replication is broken for too long: %s. Using destructive repair: %s",
				time.Since(app.replFailTime), app.config.Valkey.DestructiveReplicationRepairCommand))
			split := strings.Fields(app.config.Valkey.DestructiveReplicationRepairCommand)
			cmd := exec.CommandContext(app.ctx, split[0], split[1:]...)
			err := cmd.Run()
			if err != nil {
				app.logger.Error("Unable to run destructive replication repair on local node", slog.Any("error", err))
			} else {
				app.replFailTime = time.Now()
			}
		}
	}
	if !replicates(masterState, rs, replicaFQDN, masterNode, true) {
		app.logger.Info("Initiating replica repair", slog.String("fqdn", replicaFQDN))
		switch app.mode {
		case modeSentinel:
			err := node.SentinelMakeReplica(app.ctx, master)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to make %s replica of %s", node.FQDN(), master), slog.Any("error", err))
			}
		case modeCluster:
			alone, err := node.IsClusterNodeAlone(app.ctx)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to check if %s is alone", node.FQDN()), slog.Any("error", err))
				return
			}
			if alone {
				masterIP, err := masterNode.GetIP()
				if err != nil {
					app.logger.Error(fmt.Sprintf("Unable to make %s replica of %s", node.FQDN(), master), slog.Any("error", err))
					return
				}
				err = node.ClusterMeet(app.ctx, masterIP, app.config.Valkey.Port, app.config.Valkey.ClusterBusPort)
				if err != nil {
					app.logger.Error(fmt.Sprintf("Unable to make %s meet with master %s at %s:%d:%d", node.FQDN(), master, masterIP, app.config.Valkey.Port, app.config.Valkey.ClusterBusPort), slog.Any("error", err))
					return
				}
			}
			masterID, err := masterNode.ClusterGetID(app.ctx)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to get cluster id of %s", master), slog.Any("error", err))
				return
			}
			err = node.ClusterMakeReplica(app.ctx, masterID)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to make %s replica of %s (%s)", node.FQDN(), master, masterID), slog.Any("error", err))
			}
		}
	}
	if state.IsReplPaused {
		err := node.ResumeReplication(app.ctx)
		if err != nil {
			app.logger.Error("Unable to resume replication", slog.String("fqdn", node.FQDN()), slog.Any("error", err))
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
		app.logger.Warn(fmt.Sprintf("Local node has %d free connections left. Killing all client connections.", freeConns))
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
		app.logger.Error("Unable to get local node offline state", slog.Any("error", err))
		if app.nodeFailTime[local.FQDN()].IsZero() {
			app.nodeFailTime[local.FQDN()] = time.Now()
		}
		failedTime := time.Since(app.nodeFailTime[local.FQDN()])
		if failedTime > app.config.Valkey.BusyTimeout && strings.HasPrefix(err.Error(), "BUSY ") {
			err = local.ScriptKill(app.ctx)
			if err != nil {
				app.logger.Error("Local node is busy running a script. But SCRIPT KILL failed", slog.Any("error", err))
			}
		}
		if strings.HasPrefix(err.Error(), "LOADING ") {
			app.nodeFailTime[local.FQDN()] = time.Now()
		} else if failedTime > app.config.Valkey.RestartTimeout {
			app.nodeFailTime[local.FQDN()] = time.Now()
			err = local.Restart(app.ctx)
			if err != nil {
				app.logger.Error("Unable to restart local node", slog.Any("error", err))
			}
		}
	} else if !offline {
		delete(app.nodeFailTime, local.FQDN())
	}

	if !offline {
		err = app.adjustAofMode(master)
		if err != nil {
			app.logger.Error("Unable to adjust aof config on local node", slog.Any("error", err))
		}
		err = app.closeStaleReplica(master)
		if err != nil {
			app.logger.Error("Unable to close local node on staleness", slog.Any("error", err))
		}
		err = app.reservedConnectionsWatchdog(info)
		if err != nil {
			app.logger.Error("Unable to run reserved connections watchdog", slog.Any("error", err))
		}
		return true
	}

	shardState, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error("Local repair: unable to get actual shard state", slog.Any("error", err))
		return false
	}
	state, ok := shardState[local.FQDN()]
	if !ok {
		app.logger.Error("Local repair: unable to find local node in shard state")
		return true
	}
	if master == local.FQDN() && len(shardState) != 1 {
		activeNodes, err := app.GetActiveNodes()
		if err != nil {
			app.logger.Error("Unable to get active nodes for local node repair", slog.Any("error", err))
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
					app.logger.Warn(fmt.Sprintf("Host %s is ahead in replication history", host))
					aheadHosts++
				}
			}
			if aheadHosts != 0 {
				app.logger.Error(fmt.Sprintf("Not making local node online: %d nodes are ahead in replication history", aheadHosts))
				return false
			}
		}
	} else if master == local.FQDN() {
		if !state.IsMaster {
			app.logger.Error("Local node is alone in shard and is replica. Promoting")
			if err := app.promote(master, master, shardState, time.Now().Add(app.config.Valkey.WaitPromoteForceTimeout)); err != nil {
				app.logger.Error("Unable to promote lone node in shard", slog.Any("error", err))
				return false
			}
		}
	} else if app.isReplicaStale(state.ReplicaState, true) {
		shardState, err := app.getShardStateFromDcs()
		if err != nil {
			app.logger.Error("Unable to get shard state from dcs on slate replica open", slog.Any("error", err))
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
				app.logger.Info("Repairing local replica as it is offline and not replicates from primary")
				app.repairReplica(local, shardState[master], state, master, local.FQDN())
			} else {
				app.logger.Error(fmt.Sprintf("Leaving local offline replica broken: currently syncing %d/%d", syncing, app.config.Valkey.MaxParallelSyncs))
			}
		}
		if shardState[master].PingOk && shardState[master].PingStable && time.Since(shardState[master].CheckAt) < 3*app.config.HealthCheckInterval {
			app.logger.Error("Not making local node online: considered stale")
			return false
		}
	}
	err = local.SetOnline(app.ctx)
	if err != nil {
		app.logger.Error("Unable to set local node online", slog.Any("error", err))
		return false
	}
	if !app.dcsDivergeTime.IsZero() {
		app.logger.Info("Clearing DCS divergence time state")
		app.dcsDivergeTime = time.Time{}
	}
	return true
}
