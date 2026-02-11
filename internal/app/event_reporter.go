package app

import (
	"context"
	"fmt"
	"log/slog"
	"os/exec"
	"strings"
	"time"

	"github.com/yandex/rdsync/internal/config"
)

// timingReporter handles reporting event durations to an external program
type timingReporter struct {
	command string
	argsFmt []string
	logger  *slog.Logger
}

func newTimingReporter(conf *config.Config, logger *slog.Logger) *timingReporter {
	if conf.EventTimingNotifyCommand == "" {
		return nil
	}
	return &timingReporter{
		command: conf.EventTimingNotifyCommand,
		argsFmt: conf.EventTimingNotifyArgs,
		logger:  logger,
	}
}

// reportTiming sends an event duration to the external program asynchronously.
// If the reporter is nil (not configured), this is a no-op.
// Never blocks the caller — runs in a separate goroutine.
func (r *timingReporter) reportTiming(eventType string, duration time.Duration) {
	if r == nil {
		return
	}
	go r.send(eventType, duration)
}

func (r *timingReporter) send(eventType string, duration time.Duration) {
	// Build placeholder map
	replacements := map[string]string{
		"{event}":       eventType,
		"{duration_ms}": fmt.Sprintf("%d", duration.Milliseconds()),
	}

	// Apply replacements to each argument
	args := make([]string, len(r.argsFmt))
	for i, argTemplate := range r.argsFmt {
		arg := argTemplate
		for placeholder, value := range replacements {
			arg = strings.ReplaceAll(arg, placeholder, value)
		}
		args[i] = arg
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, r.command, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		r.logger.Warn("Timing reporter: external command failed",
			slog.String("event", eventType),
			slog.Any("error", err),
			slog.String("output", string(output)),
			slog.String("command", r.command),
			slog.Any("args", args))
	}
}
