package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

func (app *App) getPoisonPill() (*PoisonPill, error) {
	var poisonPill PoisonPill
	err := app.dcs.Get(pathPoisonPill, &poisonPill)
	if err != nil {
		return nil, err
	}
	return &poisonPill, err
}

func (app *App) issuePoisonPill(targetHost string) error {
	poisonPill := &PoisonPill{
		TargetHost:  targetHost,
		InitiatedBy: app.config.Hostname,
		InitiatedAt: time.Now(),
	}

	return app.dcs.Create(pathPoisonPill, poisonPill)
}

func (app *App) applyPoisonPill(poisonPill *PoisonPill) error {
	if poisonPill.TargetHost != app.config.Hostname {
		app.logger.Info(fmt.Sprintf("Poison pill issued for %s: not local host", poisonPill.TargetHost))
		return nil
	}
	local := app.shard.Local()
	isOffline, err := local.IsOffline(app.ctx)
	if err != nil {
		app.logger.Error("Unable to check offline status for poison pill apply", slog.Any("error", err))
		return local.Restart(app.ctx)
	}
	if !isOffline {
		app.logger.Info(fmt.Sprintf("Applying poison pill issued by %s: Going offline", poisonPill.InitiatedBy))
		err = local.SetOffline(app.ctx)
		if err != nil {
			return err
		}
	}
	poisonPill.Applied = true
	return app.dcs.Set(pathPoisonPill, poisonPill)
}

func (app *App) clearPoisonPill() error {
	return app.dcs.Delete(pathPoisonPill)
}

func (app *App) waitPoisonPill(timeout time.Duration) {
	waitCtx, cancel := context.WithTimeout(app.ctx, timeout)
	defer cancel()
	var poisonPill PoisonPill
	ticker := time.NewTicker(time.Second)
Out:
	for {
		select {
		case <-ticker.C:
			err := app.dcs.Get(pathPoisonPill, &poisonPill)
			if err != nil {
				app.logger.Error("Wait for poison pill apply", slog.Any("error", err))
			}
			err = app.applyPoisonPill(&poisonPill)
			if err != nil {
				app.logger.Error("Poison pill apply", slog.Any("error", err))
			}
			if poisonPill.Applied {
				break Out
			}
		case <-waitCtx.Done():
			break Out
		}
	}
	if !poisonPill.Applied {
		app.logger.Error(fmt.Sprintf("Poison pill for %s was not applied within timeout", poisonPill.TargetHost))
	}
}
