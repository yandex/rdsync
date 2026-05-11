package app

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gofrs/flock"
	"github.com/rs/zerolog"

	"github.com/yandex/rdsync/internal/config"
	"github.com/yandex/rdsync/internal/dcs"
	"github.com/yandex/rdsync/internal/valkey"
)

// App is main application structure
type App struct {
	dcsDivergeTime time.Time
	replFailTime   time.Time
	lostSince      time.Time
	critical       atomic.Value
	ctx            context.Context
	dcs            dcs.DCS
	config         *config.Config
	splitTime      map[string]time.Time
	logger         *zerolog.Logger
	loggerCloser   io.Closer
	nodeFailTime   map[string]time.Time
	shard          *valkey.Shard
	cache          *valkey.SentiCacheNode
	daemonLock     *flock.Flock
	timings        *TimingReporter
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
	logger, loggerCloser := newMainLogger(logLevelN, conf.LogBufferSize, conf.LogPollInterval)
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
		loggerCloser: loggerCloser,
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

func (app *App) reconnectDCS() error {
	app.logger.Info().Msg("Attempting DCS reconnection after prolonged Lost state")
	oldDCS := app.dcs
	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("DCS reconnection failed")
		app.dcs = oldDCS
		return err
	}
	app.dcs.SetDisconnectCallback(func() error { return app.handleCritical() })
	app.shard.SetDCS(app.dcs)
	oldDCS.Close()
	app.logger.Info().Msg("DCS reconnection successful")
	return nil
}

// CloseLogger drains the logger's queue
func (app *App) CloseLogger() {
	app.loggerCloser.Close()
}

func (app *App) lockDaemonFile() {
	app.daemonLock = flock.New(app.config.DaemonLockFile)
	if locked, err := app.daemonLock.TryLock(); !locked {
		msg := "another instance is running."
		if err != nil {
			msg = err.Error()
		}
		app.logger.Error().Str("error", msg).Msgf("Unable to acquire daemon lock on %s", app.config.DaemonLockFile)
		app.CloseLogger()
		os.Exit(1)
	}
}

func (app *App) unlockDaemonFile() {
	err := app.daemonLock.Unlock()
	if err != nil {
		app.logger.Error().Err(err).Msgf("Unable to unlock daemon lock %s", app.config.DaemonLockFile)
	}
}

// Run enters the main application loop
func (app *App) Run() int {
	app.lockDaemonFile()
	defer app.unlockDaemonFile()
	defer app.loggerCloser.Close()

	app.timings = newTimingReporter(app.config, app.logger)
	defer app.timings.Close()

	err := app.connectDCS()
	if err != nil {
		app.logger.Error().Err(err).Msg("Unable to connect to dcs")
		return 1
	}
	defer app.dcs.Close()
	app.dcs.SetDisconnectCallback(func() error { return app.handleCritical() })

	app.shard = valkey.NewShard(app.config, app.logger, app.dcs)
	defer app.shard.Close()
	if app.mode == modeSentinel {
		app.cache, err = valkey.NewSentiCacheNode(app.config, app.logger)
		if err != nil {
			app.logger.Error().Err(err).Msg("Unable to init senticache node")
			return 1
		}
		defer app.cache.Close()
		go app.cacheUpdater()
	}

	go app.pprofHandler()
	go app.healthChecker()
	go app.stateFileHandler()

	sighup := make(chan os.Signal, 1)
	signal.Notify(sighup, syscall.SIGHUP)

	ticker := time.NewTicker(app.config.TickInterval)
	for {
		select {
		case <-sighup:
			app.timings.Reopen()
		case <-ticker.C:
			for {
				app.logger.Info().Msgf("Rdsync state: %s", app.state)
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
				if nextState == stateLost && app.state != stateLost {
					app.lostSince = time.Now()
				} else if nextState != stateLost && app.state == stateLost {
					app.lostSince = time.Time{}
				}
				app.state = nextState
			}
		case <-app.ctx.Done():
			return 0
		}
	}
}
