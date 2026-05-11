package app

import (
	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) stateCandidate() appState {
	if !app.dcs.IsConnected() {
		return stateLost
	}
	err := app.shard.UpdateHostsInfo()
	if err != nil {
		app.logger.Error().Err(err).Msg("Candidate: failed to update host info from DCS")
		return stateCandidate
	}
	shardState, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error().Err(err).Msg("Failed to get shard state from DB")
	} else {
		app.logger.Info().Msgf("Shard state: %v", shardState)
	}
	maintenance, err := app.GetMaintenance()
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error().Err(err).Msg("Candidate: failed to get maintenance from DCS")
		return stateCandidate
	}
	if maintenance != nil && maintenance.RdSyncPaused {
		return stateMaintenance
	}

	poisonPill, err := app.getPoisonPill()
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error().Err(err).Msg("Candidate: failed to get poison pill from DCS")
		return stateCandidate
	}
	if poisonPill != nil {
		err = app.applyPoisonPill(poisonPill)
		if err != nil {
			app.logger.Error().Err(err).Msg("Candidate: failed to apply poison pill")
			return stateCandidate
		}
		if poisonPill.TargetHost == app.config.Hostname {
			return stateCandidate
		}
	}

	var master string
	err = app.dcs.Get(pathMasterNode, &master)
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error().Err(err).Msg("Candidate: failed to get current master from DCS")
		return stateCandidate
	}
	app.repairLocalNode(master)

	if app.dcs.AcquireLock(pathManagerLock) {
		return stateManager
	}
	return stateCandidate
}
