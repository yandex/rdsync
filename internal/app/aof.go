package app

import (
	"os"
)

func (app *App) adjustAofMode(master string) error {
	if app.aofMode == modeUnspecified {
		return nil
	}
	local := app.shard.Local()
	targetMode := true
	if app.aofMode == modeOff {
		targetMode = false
	} else if app.aofMode == modeOnReplicas && local.FQDN() == master && app.checkHAReplicasRunning() {
		targetMode = false
	}
	currentMode, err := local.GetAppendonly(app.ctx)
	if err != nil {
		return err
	}
	if currentMode != targetMode {
		err = local.SetAppendonly(app.ctx, targetMode)
		if err != nil {
			return err
		}
	}
	if app.config.Valkey.AofPath != "" && !targetMode {
		if _, err := os.Stat(app.config.Valkey.AofPath); err == nil {
			return os.RemoveAll(app.config.Valkey.AofPath)
		}
	}
	return nil
}
