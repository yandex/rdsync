package app

import (
	"fmt"
	"time"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) getLocalState() *HostState {
	node := app.shard.Local()
	if node == nil {
		return nil
	}
	return app.getHostState(node.FQDN())
}

func (app *App) healthChecker() {
	ticker := time.NewTicker(app.config.HealthCheckInterval)
	for {
		select {
		case <-ticker.C:
			hc := app.getLocalState()
			app.logger.Info(fmt.Sprintf("healthcheck: %v", hc))
			if hc != nil {
				err := app.dcs.SetEphemeral(dcs.JoinPath(pathHealthPrefix, app.config.Hostname), hc)
				if err != nil {
					app.logger.Error("Failed to set healthcheck status to dcs", "error", err)
				}
			}
		case <-app.ctx.Done():
			return
		}
	}
}
