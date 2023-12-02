package app

import (
	"os"
)

func (app *App) handleCritical() error {
	if app.critical.Load().(bool) {
		app.logger.Error("Lost dcs connection in critical section")
		os.Exit(1)
	} else {
		app.logger.Info("Lost dcs connection in non-critical section")
	}
	return nil
}

func (app *App) enterCritical() {
	app.critical.Store(true)
}

func (app *App) exitCritical() {
	app.critical.Store(false)
}
