package app

import (
	"fmt"
	"log/slog"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) stateCandidate() appState {
	if !app.dcs.IsConnected() {
		return stateLost
	}
	err := app.shard.UpdateHostsInfo()
	if err != nil {
		app.logger.Error("Candidate: failed to update host info from DCS", slog.Any("error", err))
		return stateCandidate
	}
	shardState, err := app.getShardStateFromDB()
	if err != nil {
		app.logger.Error("Failed to get shard state from DB", slog.Any("error", err))
	} else {
		app.logger.Info(fmt.Sprintf("Shard state: %v", shardState))
	}
	maintenance, err := app.GetMaintenance()
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error("Candidate: failed to get maintenance from DCS", slog.Any("error", err))
		return stateCandidate
	}
	if maintenance != nil && maintenance.RdSyncPaused {
		return stateMaintenance
	}

	poisonPill, err := app.getPoisonPill()
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error("Candidate: failed to get poison pill from DCS", slog.Any("error", err))
		return stateCandidate
	}
	if poisonPill != nil {
		err = app.applyPoisonPill(poisonPill)
		if err != nil {
			app.logger.Error("Candidate: failed to apply poison pill", slog.Any("error", err))
			return stateCandidate
		}
		if poisonPill.TargetHost == app.config.Hostname {
			return stateCandidate
		}
	}

	var master string
	err = app.dcs.Get(pathMasterNode, &master)
	if err != nil && err != dcs.ErrNotFound {
		app.logger.Error("Candidate: failed to get current master from DCS", slog.Any("error", err))
		return stateCandidate
	}
	app.repairLocalNode(master)

	if app.dcs.AcquireLock(pathManagerLock) {
		return stateManager
	}
	return stateCandidate
}
