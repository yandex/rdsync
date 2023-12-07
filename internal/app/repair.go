package app

import (
	"fmt"
	"strings"
	"time"

	"github.com/yandex/rdsync/internal/redis"
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
			if syncing < app.config.Redis.MaxParallelSyncs {
				app.repairReplica(node, masterState, state, master, host)
				syncing++
			} else {
				app.logger.Error(fmt.Sprintf("Leaving replica %s broken: currently syncing %d/%d", host, syncing, app.config.Redis.MaxParallelSyncs))
			}
		}
	}
}

func (app *App) repairMaster(node *redis.Node, activeNodes []string, state *HostState) {
	expectedMinReplicas := app.getMinReplicasToWrite(activeNodes)
	actualMinReplicas, err := node.GetMinReplicas(app.ctx)
	if err != nil {
		app.logger.Error("Unable to get actual min replicas on master", "fqdn", node.FQDN(), "error", err)
		return
	}
	if actualMinReplicas != expectedMinReplicas {
		app.logger.Info(fmt.Sprintf("Changing min replicas from %d to %d on master", actualMinReplicas, expectedMinReplicas), "fqdn", node.FQDN())
		err, rewriteErr := node.SetMinReplicas(app.ctx, expectedMinReplicas)
		if err != nil {
			app.logger.Error("Unable to set min replicas on master", "fqdn", node.FQDN(), "error", err)
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

func (app *App) repairReplica(node *redis.Node, masterState, state *HostState, master, replicaFQDN string) {
	masterNode := app.shard.Get(master)
	rs := state.ReplicaState
	if !replicates(masterState, rs, replicaFQDN, masterNode, true) {
		app.logger.Info("Initiating replica repair", "fqdn", replicaFQDN)
		switch app.mode {
		case modeSentinel:
			err := node.SentinelMakeReplica(app.ctx, master)
			if err != nil {
				app.logger.Error(fmt.Sprintf("Unable to make %s replica of %s", node.FQDN(), master), "error", err)
			}
		case modeCluster:
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

func (app *App) repairLocalNode(shardState map[string]*HostState, master string) {
	local := app.shard.Local()

	offline, err := local.IsOffline(app.ctx)
	if err != nil {
		app.logger.Error("Unable to get local node offline state", "error", err)
		if app.nodeFailTime[local.FQDN()].IsZero() {
			app.nodeFailTime[local.FQDN()] = time.Now()
		}
		failedTime := time.Since(app.nodeFailTime[local.FQDN()])
		if failedTime > app.config.Redis.RestartTimeout && !strings.HasPrefix(err.Error(), "LOADING ") {
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
		return
	}

	state, ok := shardState[local.FQDN()]
	if !ok {
		app.logger.Error("Local repair: unable to find local node in shard state")
		return
	}
	if master == local.FQDN() && len(shardState) != 1 {
		activeNodes, err := app.GetActiveNodes()
		if err != nil {
			app.logger.Error("Unable to get active nodes for local node repair", "error", err)
			return
		}
		activeSet := make(map[string]bool, len(activeNodes))
		for _, node := range activeNodes {
			activeSet[node] = true
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
				return
			}
		}
	} else if state.ReplicaState == nil {
		err, rewriteErr := local.SetReadOnly(app.ctx, false)
		if err != nil {
			app.logger.Error("Unable to make local node read-only", "error", err)
			return
		}
		if rewriteErr != nil {
			app.logger.Error("Unable rewrite conf after making local node read-only", "error", rewriteErr)
			return
		}
	}
	err = local.SetOnline(app.ctx)
	if err != nil {
		app.logger.Error("Unable to set local node online", "error", err)
	}
}
