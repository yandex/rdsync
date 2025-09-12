package app

import (
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
)

func (app *App) pprofHandler() {
	if app.config.PprofAddr == "" {
		return
	}
	serverMux := http.NewServeMux()
	serverMux.HandleFunc("/pprof/", pprof.Index)
	serverMux.HandleFunc("/pprof/cmdline", pprof.Cmdline)
	serverMux.HandleFunc("/pprof/profile", pprof.Profile)
	serverMux.HandleFunc("/pprof/symbol", pprof.Symbol)
	serverMux.HandleFunc("/pprof/trace", pprof.Trace)
	serverMux.HandleFunc("/pprof/heap", pprof.Handler("heap").ServeHTTP)
	serverMux.HandleFunc("/pprof/goroutine", pprof.Handler("goroutine").ServeHTTP)

	err := http.ListenAndServe(app.config.PprofAddr, serverMux)
	if err != nil {
		app.logger.Error("Unable to init pprof handler", slog.Any("error", err))
		os.Exit(1)
	}
}
