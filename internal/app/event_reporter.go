package app

import (
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/yandex/rdsync/internal/config"
)

// TimingReporter handles reporting event durations to a separate log file
type TimingReporter struct {
	logger    *slog.Logger
	appLogger *slog.Logger
	file      *os.File
	path      string
	mu        sync.Mutex
}

func openTimingLog(path string) (*os.File, *slog.Logger, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, err
	}
	logger := slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo}))
	return f, logger, nil
}

func newTimingReporter(conf *config.Config, appLogger *slog.Logger) *TimingReporter {
	if conf.EventTimingLogFile == "" {
		return nil
	}

	f, logger, err := openTimingLog(conf.EventTimingLogFile)
	if err != nil {
		appLogger.Error("Failed to open event timing log file", slog.String("path", conf.EventTimingLogFile), slog.Any("error", err))
		return nil
	}

	return &TimingReporter{
		logger:    logger,
		appLogger: appLogger,
		file:      f,
		path:      conf.EventTimingLogFile,
	}
}

// reportTiming logs an event duration to the timing log file.
// If the reporter is nil (not configured), this is a no-op.
func (r *TimingReporter) reportTiming(eventType string, duration time.Duration) {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	r.logger.Info("event_timing", slog.String("event", eventType), slog.Int64("duration_ms", duration.Milliseconds()))
}

// Reopen closes the current log file and opens it again at the same path.
// This supports log rotation: an external tool renames the file, then sends
// SIGHUP, and the reporter starts writing to a new file at the original path.
// If the reporter is nil (not configured), this is a no-op.
func (r *TimingReporter) Reopen() {
	if r == nil {
		return
	}

	r.appLogger.Info("Reopening timing log file")

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file != nil {
		r.file.Close()
	}

	f, logger, err := openTimingLog(r.path)
	if err != nil {
		r.appLogger.Error("Failed to reopen event timing log file", slog.String("path", r.path), slog.Any("error", err))
		r.file = nil
		r.logger = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
		return
	}

	r.file = f
	r.logger = logger
}

// Close shuts down the reporter and closes the log file
func (r *TimingReporter) Close() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
}
