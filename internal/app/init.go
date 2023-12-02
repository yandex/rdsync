package app

func (app *App) stateInit() appState {
	if !app.dcs.WaitConnected(app.config.DcsWaitTimeout) {
		if app.doesMaintenanceFileExist() {
			return stateMaintenance
		}
		return stateInit
	}
	app.dcs.Initialize()
	if app.dcs.AcquireLock(pathManagerLock) {
		return stateManager
	}
	return stateCandidate
}
