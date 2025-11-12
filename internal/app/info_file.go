package app

import (
	"encoding/json"
	"log/slog"
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
				app.logger.Error("StateFileHandler: failed to get current zk tree", slog.Any("error", err))
				_ = os.Remove(app.config.InfoFile)
				continue
			}
			data, err := json.Marshal(tree)
			if err != nil {
				app.logger.Error("StateFileHandler: failed to marshal zk node data", slog.Any("error", err))
				_ = os.Remove(app.config.InfoFile)
				continue
			}
			err = os.WriteFile(app.config.InfoFile, data, 0o640)
			if err != nil {
				app.logger.Error("StateFileHandler: failed to write info file", slog.Any("error", err))
				_ = os.Remove(app.config.InfoFile)
				continue
			}

		case <-app.ctx.Done():
			return
		}
	}
}
