package app

import (
	"io"
	"os"
	"sync"
	"time"

	"github.com/rs/zerolog"

	"github.com/yandex/rdsync/internal/config"
)

// TimingReporter handles reporting event durations to a separate log file
type TimingReporter struct {
	logger       *zerolog.Logger
	appLogger    *zerolog.Logger
	loggerCloser io.Closer
	file         *os.File
	path         string
	bufSize      int
	pollInterval time.Duration
	mu           sync.Mutex
}

func openTimingLog(path string, bufSize int, pollInterval time.Duration) (*os.File, *zerolog.Logger, io.Closer, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, nil, nil, err
	}
	logger, closer := newEventLogger(f, bufSize, pollInterval)
	return f, logger, closer, nil
}

func newTimingReporter(conf *config.Config, appLogger *zerolog.Logger) *TimingReporter {
	if conf.EventTimingLogFile == "" {
		return nil
	}

	f, logger, closer, err := openTimingLog(conf.EventTimingLogFile, conf.LogBufferSize, conf.LogPollInterval)
	if err != nil {
		appLogger.Error().Err(err).Str("path", conf.EventTimingLogFile).Msg("Failed to open event timing log file")
		return nil
	}

	return &TimingReporter{
		logger:       logger,
		appLogger:    appLogger,
		loggerCloser: closer,
		file:         f,
		path:         conf.EventTimingLogFile,
		bufSize:      conf.LogBufferSize,
		pollInterval: conf.LogPollInterval,
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

	r.logger.Info().Str("event", eventType).Int64("duration_ms", duration.Milliseconds()).Msg("event_timing")
}

// Reopen closes the current log file and opens it again at the same path.
// This supports log rotation: an external tool renames the file, then sends
// SIGHUP, and the reporter starts writing to a new file at the original path.
// If the reporter is nil (not configured), this is a no-op.
func (r *TimingReporter) Reopen() {
	if r == nil {
		return
	}

	r.appLogger.Info().Msg("Reopening timing log file")

	r.mu.Lock()
	defer r.mu.Unlock()

	// Flush and close the old diode before closing the file.
	if r.loggerCloser != nil {
		r.loggerCloser.Close()
		r.loggerCloser = nil
	}
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}

	f, logger, closer, err := openTimingLog(r.path, r.bufSize, r.pollInterval)
	if err != nil {
		r.appLogger.Error().Err(err).Str("path", r.path).Msg("Failed to reopen event timing log file")
		// Fall back to a no-op logger so subsequent reportTiming calls don't panic.
		nop := zerolog.Nop()
		r.logger = &nop
		return
	}

	r.file = f
	r.logger = logger
	r.loggerCloser = closer
}

// Close shuts down the reporter and closes the log file
func (r *TimingReporter) Close() {
	if r == nil {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if r.loggerCloser != nil {
		r.loggerCloser.Close()
		r.loggerCloser = nil
	}
	if r.file != nil {
		r.file.Close()
		r.file = nil
	}
}
