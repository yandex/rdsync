package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gofrs/flock"

	"github.com/yandex/rdsync/internal/config"
	"github.com/yandex/rdsync/internal/dcs"
	"github.com/yandex/rdsync/internal/valkey"
)

// App is main application structure
type App struct {
	dcs            dcs.DCS
	critical       atomic.Value
	ctx            context.Context
	nodeFailTime   map[string]time.Time
	splitTime      map[string]time.Time
	dcsDivergeTime time.Time
	replFailTime   time.Time
	logger         *slog.Logger
	config         *config.Config
	shard          *valkey.Shard
	cache          *valkey.SentiCacheNode
	daemonLock     *flock.Flock
	mode           appMode
	aofMode        aofMode
	state          appState
}

func baseContext() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-signals
		cancel()
	}()
	return ctx
}

func parseLevel(level string) (slog.Level, error) {
	switch level {
	case "Debug":
		return slog.LevelDebug, nil
	case "Info":
		return slog.LevelInfo, nil
	case "Warn":
		return slog.LevelWarn, nil
	case "Error":
		return slog.LevelError, nil
	}
	return slog.LevelInfo, fmt.Errorf("unknown error level: %s", level)
}

// NewApp is an App constructor
func NewApp(configFile, logLevel string) (*App, error) {
	conf, err := config.ReadFromFile(configFile)
	if err != nil {
		return nil, err
	}
	effectiveLevel := logLevel
	if effectiveLevel == "" {
		effectiveLevel = conf.LogLevel
	}
	logLevelN, err := parseLevel(effectiveLevel)
	if err != nil {
		return nil, err
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: logLevelN}))
	mode, err := parseMode(conf.Mode)
	if err != nil {
		return nil, err
	}
	aofMode, err := parseAofMode(conf.AofMode)
	if err != nil {
		return nil, err
	}
	app := &App{
		ctx:          baseContext(),
		mode:         mode,
		aofMode:      aofMode,
		nodeFailTime: make(map[string]time.Time),
		splitTime:    make(map[string]time.Time),
		state:        stateInit,
		logger:       logger,
		config:       conf,
	}
	app.critical.Store(false)
	return app, nil
}

func (app *App) connectDCS() error {
	var err error
	app.dcs, err = dcs.NewZookeeper(app.ctx, &app.config.Zookeeper, app.logger)
	if err != nil {
		return fmt.Errorf("failed to connect to zkDCS: %s", err.Error())
	}
	return nil
}

func (app *App) lockDaemonFile() {
	app.daemonLock = flock.New(app.config.DaemonLockFile)
	if locked, err := app.daemonLock.TryLock(); !locked {
		msg := "another instance is running."
		if err != nil {
			msg = err.Error()
		}
		app.logger.Error(fmt.Sprintf("Unable to acquire daemon lock on %s", app.config.DaemonLockFile), slog.Any("error", msg))
		os.Exit(1)
	}
}

func (app *App) unlockDaemonFile() {
	err := app.daemonLock.Unlock()
	if err != nil {
		app.logger.Error(fmt.Sprintf("Unable to unlock daemon lock %s", app.config.DaemonLockFile), slog.Any("error", err))
	}
}

// Run enters the main application loop
func (app *App) Run() int {
	app.lockDaemonFile()
	defer app.unlockDaemonFile()
	err := app.connectDCS()
	if err != nil {
		app.logger.Error("Unable to connect to dcs", slog.Any("error", err))
		return 1
	}
	defer app.dcs.Close()
	app.dcs.SetDisconnectCallback(func() error { return app.handleCritical() })

	app.shard = valkey.NewShard(app.config, app.logger, app.dcs)
	defer app.shard.Close()
	if app.mode == modeSentinel {
		app.cache, err = valkey.NewSentiCacheNode(app.config, app.logger)
		if err != nil {
			app.logger.Error("Unable to init senticache node", slog.Any("error", err))
			return 1
		}
		defer app.cache.Close()
		go app.cacheUpdater()
	}

	go app.pprofHandler()
	go app.healthChecker()
	go app.stateFileHandler()

	ticker := time.NewTicker(app.config.TickInterval)
	for {
		select {
		case <-ticker.C:
			for {
				app.logger.Info(fmt.Sprintf("Rdsync state: %s", app.state))
				stateHandler := map[appState](func() appState){
					stateInit:        app.stateInit,
					stateManager:     app.stateManager,
					stateCandidate:   app.stateCandidate,
					stateLost:        app.stateLost,
					stateMaintenance: app.stateMaintenance,
				}[app.state]
				if stateHandler == nil {
					panic(fmt.Sprintf("Unknown state: %s", app.state))
				}
				nextState := stateHandler()
				if nextState == app.state {
					break
				}
				app.state = nextState
			}
		case <-app.ctx.Done():
			return 0
		}
	}
}
