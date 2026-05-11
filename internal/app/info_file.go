package app

import (
	json "encoding/json/v2"
	"os"
	"time"
)

func (app *App) stateFileHandler() {
	ticker := time.NewTicker(app.config.InfoFileHandlerInterval)
	for {
		select {
		case <-ticker.C:
			tree, err := app.dcs.GetTree("")
			if err != nil {
				app.logger.Error().Err(err).Msg("StateFileHandler: failed to get current zk tree")
				_ = os.Remove(app.config.InfoFile)
				continue
			}
			data, err := json.Marshal(tree)
			if err != nil {
				app.logger.Error().Err(err).Msg("StateFileHandler: failed to marshal zk node data")
				_ = os.Remove(app.config.InfoFile)
				continue
			}
			err = os.WriteFile(app.config.InfoFile, data, 0o640)
			if err != nil {
				app.logger.Error().Err(err).Msg("StateFileHandler: failed to write info file")
				_ = os.Remove(app.config.InfoFile)
				continue
			}

		case <-app.ctx.Done():
			return
		}
	}
}
