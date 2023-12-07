package app

import (
	"slices"

	"github.com/yandex/rdsync/internal/redis"
)

func replicates(masterState *HostState, replicaState *ReplicaState, replicaFQDN string, masterNode *redis.Node, allowSync bool) bool {
	if replicaState == nil || !(replicaState.MasterLinkState || allowSync) {
		return false
	}
	if slices.Contains(masterState.ConnectedReplicas, replicaFQDN) {
		return true
	}
	return masterNode.MatchHost(replicaState.MasterHost)
}
