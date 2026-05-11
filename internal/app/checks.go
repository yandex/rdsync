package app

func (app *App) checkHAReplicasRunning() bool {
	hosts := len(app.shard.Hosts())
	if hosts == 1 {
		app.logger.Info().Msg("Check HA replicas ok: single node mode")
		return true
	}
	state, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error().Err(err).Msg("Check HA replicas failed")
		return false
	}

	local := app.shard.Local()
	localState, ok := state[local.FQDN()]
	if !ok {
		app.logger.Error().Str("fqdn", local.FQDN()).Msg("Unable to find local node in state")
		return false
	}

	baseOffset := getOffset(localState)

	aheadHosts := 0
	availableReplicas := 0
	for host, hostState := range state {
		if getOffset(hostState) > baseOffset {
			app.logger.Warn().Str("fqdn", host).Msg("Host is ahead in replication history")
			aheadHosts++
		}
		if hostState.PingOk && !hostState.IsMaster {
			if replicates(localState, hostState.ReplicaState, host, local, false) {
				availableReplicas++
			}
		}
	}

	if aheadHosts > 0 {
		app.logger.Error().Msgf("Not making local node online: %d nodes are ahead in replication history", aheadHosts)
	}

	if availableReplicas >= hosts/2 {
		app.logger.Info().Msgf("Check HA replicas ok: %d replicas available", availableReplicas)
		return true
	}
	app.logger.Error().Msgf("Check HA replicas failed: %d replicas available", availableReplicas)
	return false
}
