package app

import (
	"errors"
	"fmt"

	"github.com/yandex/rdsync/internal/dcs"
)

func (app *App) hasData(state *HostState) bool {
	return state != nil && state.KeysCount > app.config.Valkey.MinKeys
}

func (app *App) lacksData(state *HostState) bool {
	return app.masterInfo != nil && app.masterInfo.HasKeys && !app.hasData(state)
}

func (app *App) getMasterInfo() (*MasterInfo, error) {
	var masterInfo MasterInfo
	err := app.dcs.Get(pathMasterInfo, &masterInfo)
	if err != nil {
		if errors.Is(err, dcs.ErrNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("get master info from dcs: %w", err)
	}
	return &masterInfo, nil
}

func (app *App) loadMasterInfo() error {
	if app.masterInfo != nil {
		return nil
	}
	masterInfo, err := app.getMasterInfo()
	if err != nil {
		return err
	}
	if masterInfo == nil {
		masterInfo = &MasterInfo{}
	}
	app.masterInfo = masterInfo
	return nil
}

func (app *App) setMasterInfo(masterInfo MasterInfo) error {
	err := app.dcs.Set(pathMasterInfo, masterInfo)
	if err != nil {
		return fmt.Errorf("set master info in dcs: %w", err)
	}
	app.masterInfo = &masterInfo
	return nil
}

func masterInfoNeedsWrite(current, observed MasterInfo) bool {
	if current == observed {
		return false
	}
	if current.RunID != observed.RunID && current.HasKeys && !observed.HasKeys {
		return false
	}
	return true
}

func isMasterInfoObservable(state *HostState) bool {
	return state != nil && state.PingOk && state.Error == "" && state.IsMaster && !state.IsOffline
}

func (app *App) updateMasterInfo(state *HostState) error {
	if app.masterInfo == nil {
		return fmt.Errorf("master info is not loaded")
	}
	if !isMasterInfoObservable(state) {
		return nil
	}
	observed := MasterInfo{RunID: state.RunID, HasKeys: app.hasData(state)}
	if !masterInfoNeedsWrite(*app.masterInfo, observed) {
		if *app.masterInfo != observed {
			app.logger.Error().Msgf("Master was restarted and lost its data: %v -> %v. Keeping master info.", app.masterInfo, &observed)
		}
		return nil
	}
	app.logger.Info().Msgf("Updating master info: %v -> %v", app.masterInfo, &observed)
	return app.setMasterInfo(observed)
}
