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

// timingEvent represents a single timing event to be reported
type timingEvent struct {
	eventType string
	duration  time.Duration
}

// TimingReporter handles reporting event durations to an external program
type TimingReporter struct {
	logger  *slog.Logger
	command string
	argsFmt []string
	events  chan timingEvent
	ctx     context.Context
	cancel  context.CancelFunc
}

func newTimingReporter(conf *config.Config, logger *slog.Logger) *TimingReporter {
	if conf.EventTimingNotifyCommand == "" {
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	r := &TimingReporter{
		command: conf.EventTimingNotifyCommand,
		argsFmt: conf.EventTimingNotifyArgs,
		logger:  logger,
		events:  make(chan timingEvent, 100), // buffered channel to prevent blocking
		ctx:     ctx,
		cancel:  cancel,
	}

	// Start worker goroutine to process events
	go r.worker()

	return r
}

// reportTiming sends an event duration to the external program asynchronously.
// If the reporter is nil (not configured), this is a no-op.
// Never blocks the caller — uses a buffered channel.
func (r *TimingReporter) reportTiming(eventType string, duration time.Duration) {
	if r == nil {
		return
	}

	// Non-blocking send - drop event if channel is full
	select {
	case r.events <- timingEvent{eventType: eventType, duration: duration}:
	default:
		r.logger.Warn("Timing reporter: event channel full, dropping event",
			slog.String("event", eventType))
	}
}

// Close shuts down the reporter and waits for pending events to be processed
func (r *TimingReporter) Close() {
	if r == nil {
		return
	}

	// Signal worker to stop accepting new events
	r.cancel()

	// Close the channel to signal worker to finish processing
	close(r.events)
}

// worker processes events from the channel
func (r *TimingReporter) worker() {
	for {
		select {
		case <-r.ctx.Done():
			// Drain remaining events before exiting
			for event := range r.events {
				r.send(event.eventType, event.duration)
			}
			return
		case event, ok := <-r.events:
			if !ok {
				return
			}
			r.send(event.eventType, event.duration)
		}
	}
}

func (r *TimingReporter) send(eventType string, duration time.Duration) {
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
