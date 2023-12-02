package app

import (
	"fmt"
	"strings"
)

func getOffset(state *HostState) int64 {
	if !state.PingOk || !state.PingStable {
		return 0
	}
	if state.IsMaster {
		return state.MasterReplicationOffset
	} else if state.ReplicaState != nil {
		return state.ReplicaState.ReplicationOffset
	}
	return 0
}

func isPartialSyncPossible(replica *HostState, master *HostState) bool {
	if replica == nil || master == nil {
		return false
	}
	rs := replica.ReplicaState
	if rs == nil {
		return false
	}
	psyncOffset := rs.ReplicationOffset + 1
	if (master.ReplicationID != replica.ReplicationID) && (master.ReplicationID2 != replica.ReplicationID ||
		psyncOffset > master.SecondReplicationOffset) {
		return false
	}
	if psyncOffset < master.ReplicationBacklogStart ||
		psyncOffset > (master.ReplicationBacklogStart+master.ReplicationBacklogSize) {
		return false
	}
	return true
}

func (app *App) findMostRecentNode(shardState map[string]*HostState) string {
	var recentHost string
	var recentOffset int64
	for host, state := range shardState {
		offset := getOffset(state)
		if offset > recentOffset {
			recentHost = host
			recentOffset = offset
		}
	}
	return recentHost
}

func (app *App) getMostDesirableNode(shardState map[string]*HostState, switchoverFrom string) (string, error) {
	recent := app.findMostRecentNode(shardState)
	recentState := shardState[recent]

	var recentNodes []string

	for host, state := range shardState {
		if strings.HasPrefix(host, switchoverFrom) {
			continue
		}
		if host == recent {
			recentNodes = append(recentNodes, host)
			continue
		}
		if isPartialSyncPossible(state, recentState) {
			recentNodes = append(recentNodes, host)
		}
	}

	if len(recentNodes) < 1 {
		return "", fmt.Errorf("no hosts with psync possible from most recent one: %s", recent)
	}

	app.logger.Info(fmt.Sprintf("Selecting most desirable within %s", recentNodes))

	var priorityHost string
	var maxPriority int
	var maxOffset int64

	for _, host := range recentNodes {
		nc, err := app.shard.GetNodeConfiguration(host)
		if err != nil {
			return "", err
		}
		offset := getOffset(shardState[host])
		if nc.Priority > maxPriority {
			priorityHost = host
			maxPriority = nc.Priority
			maxOffset = offset
		} else if nc.Priority == maxPriority && offset > maxOffset {
			priorityHost = host
			maxPriority = nc.Priority
			maxOffset = offset
		}
	}

	return priorityHost, nil
}
