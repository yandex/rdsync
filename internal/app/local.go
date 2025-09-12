package app

import (
	"fmt"
	"log/slog"
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
	path := dcs.JoinPath(pathHealthPrefix, app.config.Hostname)
	hcCheckTime := time.Time{}
	for {
		select {
		case <-ticker.C:
			hc := app.getLocalState()
			app.logger.Info(fmt.Sprintf("healthcheck: %v", hc))
			if hc != nil {
				hcCheckTime = hc.CheckAt
				err := app.dcs.SetEphemeral(path, hc)
				if err != nil {
					app.logger.Error("Failed to set healthcheck status to dcs", slog.Any("error", err))
				}
			} else if !hcCheckTime.IsZero() {
				if time.Since(hcCheckTime) < 5*app.config.HealthCheckInterval {
					app.logger.Warn("Unable to get local node state, leaving health node in dcs intact")
				} else {
					app.logger.Warn("Unable to get local node state, dropping health node from dcs")
					err := app.dcs.Delete(path)
					if err != nil {
						app.logger.Error("Failed to drop healthcheck status from dcs on dead local node", slog.Any("error", err))
					}
					hcCheckTime = time.Time{}
				}
			}
		case <-app.ctx.Done():
			return
		}
	}
}
