package app

import (
	"fmt"
)

func (app *App) checkHAReplicasRunning() bool {
	hosts := len(app.shard.Hosts())
	if hosts == 1 {
		app.logger.Info("Check HA replicas ok: single node mode")
		return true
	}
	state, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error("Check HA replicas failed", "error", err)
		return false
	}

	local := app.shard.Local()
	localState, ok := state[local.FQDN()]
	if !ok {
		app.logger.Error("Unable to find local node in state", "fqdn", local.FQDN())
		return false
	}

	baseOffset := getOffset(localState)

	aheadHosts := 0
	availableReplicas := 0
	for host, hostState := range state {
		if getOffset(hostState) > baseOffset {
			app.logger.Warn("Host is ahead in replication history", "fqdn", host)
			aheadHosts++
		}
		if hostState.PingOk && !hostState.IsMaster && hostState.ReplicaState != nil {
			rs := hostState.ReplicaState
			if rs.MasterLinkState && local.MatchHost(rs.MasterHost) {
				availableReplicas++
			}
		}
	}

	if aheadHosts > 0 {
		app.logger.Error(fmt.Sprintf("Not making local node online: %d nodes are ahead in replication history", aheadHosts))
	}

	if availableReplicas >= hosts/2 {
		app.logger.Info(fmt.Sprintf("Check HA replicas ok: %d replicas available", availableReplicas))
		return true
	}
	app.logger.Error(fmt.Sprintf("Check HA replicas failed: %d replicas available", availableReplicas))
	return false
}
