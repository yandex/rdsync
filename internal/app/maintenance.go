package app

import (
	"fmt"
	"os"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) enterMaintenance(maintenance *Maintenance, master string) error {
	node := app.shard.Get(master)
	err, rewriteErr := node.SetNumQuorumReplicas(app.ctx, 0)
	if err != nil {
		return err
	}
	if rewriteErr != nil {
		return rewriteErr
	}
	err = app.dcs.Delete(pathActiveNodes)
	if err != nil {
		return err
	}
	maintenance.RdSyncPaused = true
	return app.dcs.Set(pathMaintenance, maintenance)
}

func (app *App) leaveMaintenance() error {
	err := app.shard.UpdateHostsInfo()
	if err != nil {
		return err
	}
	state, err := app.getShardStateFromDB()
	if err != nil {
		return err
	}
	master, err := app.ensureCurrentMaster(state)
	if err != nil {
		return err
	}
	stateDcs, err := app.getShardStateFromDcs()
	if err != nil {
		return err
	}
	state, err = app.getShardStateFromDB()
	if err != nil {
		return err
	}
	err = app.updateActiveNodes(state, stateDcs, []string{}, master)
	if err != nil {
		return err
	}
	activeNodes, err := app.GetActiveNodes()
	if err != nil {
		return err
	}
	if len(activeNodes) == 0 {
		return fmt.Errorf("no active nodes")
	}
	app.repairShard(state, activeNodes, master)
	return app.dcs.Delete(pathMaintenance)
}

func (app *App) createMaintenanceFile() {
	err := os.WriteFile(app.config.MaintenanceFile, []byte(""), 0644)
	if err != nil {
		app.logger.Error("Failed to write maintenance file", "error", err)
	}
}

func (app *App) doesMaintenanceFileExist() bool {
	_, err := os.Stat(app.config.MaintenanceFile)
	return err == nil
}

func (app *App) removeMaintenanceFile() {
	err := os.Remove(app.config.MaintenanceFile)
	if err != nil && !os.IsNotExist(err) {
		app.logger.Error("Failed to remove maintenance file", "error", err)
	}
}

// GetMaintenance returns current maintenance status from dcs
func (app *App) GetMaintenance() (*Maintenance, error) {
	var maintenance Maintenance
	err := app.dcs.Get(pathMaintenance, &maintenance)
	if err != nil {
		return nil, err
	}
	return &maintenance, err
}

func (app *App) stateMaintenance() appState {
	if !app.doesMaintenanceFileExist() {
		app.createMaintenanceFile()
	}
	maintenance, err := app.GetMaintenance()
	if err != nil && err != dcs.ErrNotFound {
		return stateMaintenance
	}
	if err == dcs.ErrNotFound || maintenance.ShouldLeave {
		if app.dcs.AcquireLock(pathManagerLock) {
			app.logger.Info("Leaving maintenance")
			err := app.leaveMaintenance()
			if err != nil {
				app.logger.Error("Failed to leave maintenance", "error", err)
				return stateMaintenance
			}
			app.removeMaintenanceFile()
			return stateManager
		}
		app.removeMaintenanceFile()
		return stateCandidate
	}
	return stateMaintenance
}
