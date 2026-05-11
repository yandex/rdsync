package app

import (
	"context"
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
		app.logger.Info().Msgf("Poison pill issued for %s: not local host", poisonPill.TargetHost)
		return nil
	}
	local := app.shard.Local()
	isOffline, err := local.IsOffline(app.ctx)
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to check offline status for poison pill apply")
		return local.Restart(app.ctx)
	}
	if !isOffline {
		app.logger.Info().Msgf("Applying poison pill issued by %s: Going offline", poisonPill.InitiatedBy)
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
				app.logger.Error().Err(err).Msg("Wait for poison pill apply")
			}
			err = app.applyPoisonPill(&poisonPill)
			if err != nil {
				app.logger.Error().Err(err).Msg("Poison pill apply")
			}
			if poisonPill.Applied {
				break Out
			}
		case <-waitCtx.Done():
			break Out
		}
	}
	if !poisonPill.Applied {
		app.logger.Error().Msgf("Poison pill for %s was not applied within timeout", poisonPill.TargetHost)
	}
}
