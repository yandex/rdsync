package app

import (
	"fmt"
	"os/exec"
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
				app.logger.Error("Unable to make replica read-only", "fqdn", node.FQDN(), "error", err)
			}
			if rewriteErr != nil {
				app.logger.Error("Unable to rewrite config after making replica read-only", "fqdn", node.FQDN(), "error", rewriteErr)
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
			app.logger.Error("Unable to set master read-write", "fqdn", node.FQDN(), "error", err)
		}
		if rewriteErr != nil {
			app.logger.Error("Unable to rewrite config on master", "fqdn", node.FQDN(), "error", rewriteErr)
		}
	}
	expectedNumReplicas := app.getNumReplicasToWrite(activeNodes)
	actualNumReplicas, err := node.GetNumQuorumReplicas(app.ctx)
	if err != nil {
		app.logger.Error("Unable to get actual num quorum replicas on master", "fqdn", node.FQDN(), "error", err)
		return
	}
	if actualNumReplicas != expectedNumReplicas {
		app.logger.Info(fmt.Sprintf("Changing num quorum replicas from %d to %d on master", actualNumReplicas, expectedNumReplicas), "fqdn", node.FQDN())
		err, rewriteErr := node.SetNumQuorumReplicas(app.ctx, expectedNumReplicas)
		if err != nil {
			app.logger.Error("Unable to set num quorum replicas on master", "fqdn", node.FQDN(), "error", err)
		}
		if rewriteErr != nil {
			app.logger.Error("Unable to rewrite config on master", "fqdn", node.FQDN(), "error", rewriteErr)
		}
	}
	if state.IsReplPaused {
		err := node.ResumeReplication(app.ctx)
		if err != nil {
			app.logger.Error("Unable to make resume replication on master", "fqdn", node.FQDN(), "error", err)
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
				app.logger.Error("Unable to run destructive replication repair on local node", "error", err)
			} else {
				app.replFailTime = time.Now()
			}
		}
	}
	if !replicates(masterState, rs, replicaFQDN, masterNode, true) {
		app.logger.Info("Initiating replica repair", "fqdn", replicaFQDN)
		switch app.mode {
		case modeSentinel:
			err := node.SentinelMakeReplica(app.ctx, master)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to make %s replica of %s", node.FQDN(), master), "error", err)
			}
		case modeCluster:
			alone, err := node.IsClusterNodeAlone(app.ctx)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to check if %s is alone", node.FQDN()), "error", err)
				return
			}
			if alone {
				masterIP, err := masterNode.GetIP()
				if err != nil {
					app.logger.Error(fmt.Sprintf("Unable to make %s replica of %s", node.FQDN(), master), "error", err)
					return
				}
				err = node.ClusterMeet(app.ctx, masterIP, app.config.Valkey.Port, app.config.Valkey.ClusterBusPort)
				if err != nil {
					app.logger.Error(fmt.Sprintf("Unable to make %s meet with master %s at %s:%d:%d", node.FQDN(), master, masterIP, app.config.Valkey.Port, app.config.Valkey.ClusterBusPort), "error", err)
					return
				}
			}
			masterID, err := masterNode.ClusterGetID(app.ctx)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to get cluster id of %s", master), "error", err.Error())
				return
			}
			err = node.ClusterMakeReplica(app.ctx, masterID)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to make %s replica of %s (%s)", node.FQDN(), master, masterID), "error", err)
			}
		}
	}
	if state.IsReplPaused {
		err := node.ResumeReplication(app.ctx)
		if err != nil {
			app.logger.Error("Unable to resume replication", "fqdn", node.FQDN(), "error", err.Error())
		}
	}
}

func (app *App) repairLocalNode(master string) bool {
	local := app.shard.Local()

	_, _, _, offline, replPaused, err := local.GetState(app.ctx)
	if err != nil {
		app.logger.Error("Unable to get local node offline state", "error", err)
		if app.nodeFailTime[local.FQDN()].IsZero() {
			app.nodeFailTime[local.FQDN()] = time.Now()
		}
		failedTime := time.Since(app.nodeFailTime[local.FQDN()])
		if failedTime > app.config.Valkey.BusyTimeout && strings.HasPrefix(err.Error(), "BUSY ") {
			err = local.ScriptKill(app.ctx)
			if err != nil {
				app.logger.Error("Local node is busy running a script. But SCRIPT KILL failed", "error", err)
			}
		}
		if failedTime > app.config.Valkey.RestartTimeout && !strings.HasPrefix(err.Error(), "LOADING ") {
			app.nodeFailTime[local.FQDN()] = time.Now()
			err = local.Restart(app.ctx)
			if err != nil {
				app.logger.Error("Unable to restart local node", "error", err)
			}
		}
	} else if !offline {
		delete(app.nodeFailTime, local.FQDN())
	}

	if !offline {
		err = app.adjustAofMode(master)
		if err != nil {
			app.logger.Error("Unable to adjust aof config on local node", "error", err)
		}
		err = app.closeStaleReplica(master)
		if err != nil {
			app.logger.Error("Unable to close local node on staleness", "error", err)
		}
		return true
	}

	shardState, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error("Local repair: unable to get actual shard state", "error", err)
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
			app.logger.Error("Unable to get active nodes for local node repair", "error", err)
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
	} else if app.isReplicaStale(state.ReplicaState, true) {
		shardState, err := app.getShardStateFromDcs()
		if err != nil {
			app.logger.Error("Unable to get shard state from dcs on slate replica open", "error", err)
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
		app.logger.Error("Unable to set local node online", "error", err)
		return false
	}
	if !app.dcsDivergeTime.IsZero() {
		app.logger.Info("Clearing DCS divergence time state")
		app.dcsDivergeTime = time.Time{}
	}
	return true
}
