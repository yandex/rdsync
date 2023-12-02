package app

import (
	"encoding/json"
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
				app.logger.Error("StateFileHandler: failed to get current zk tree", "error", err)
				_ = os.Remove(app.config.InfoFile)
				continue
			}
			data, err := json.Marshal(tree)
			if err != nil {
				app.logger.Error("StateFileHandler: failed to marshal zk node data", "error", err)
				_ = os.Remove(app.config.InfoFile)
				continue
			}
			err = os.WriteFile(app.config.InfoFile, data, 0666)
			if err != nil {
				app.logger.Error("StateFileHandler: failed to write info file", "error", err)
				_ = os.Remove(app.config.InfoFile)
				continue
			}

		case <-app.ctx.Done():
			return
		}
	}
}
