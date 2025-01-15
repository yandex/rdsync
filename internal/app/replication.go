package app

import (
	"slices"

	"github.com/yandex/rdsync/internal/valkey"
)

func replicates(masterState *HostState, replicaState *ReplicaState, replicaFQDN string, masterNode *valkey.Node, allowSync bool) bool {
	if replicaState == nil || !(replicaState.MasterLinkState || allowSync) {
		return false
	}
	if masterState != nil && slices.Contains(masterState.ConnectedReplicas, replicaFQDN) {
		return true
	}
	return masterNode != nil && masterNode.MatchHost(replicaState.MasterHost)
}
