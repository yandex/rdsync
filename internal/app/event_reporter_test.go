package app

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/yandex/rdsync/internal/config"
)

func TestNewTimingReporterNil(t *testing.T) {
	conf := &config.Config{
		EventTimingNotifyCommand: "", // empty command
		EventTimingNotifyArgs:    []string{"{event}", "{duration_ms}"},
	}
	logger := slog.Default()

	reporter := newTimingReporter(conf, logger)
	require.Nil(t, reporter, "newTimingReporter should return nil when EventTimingNotifyCommand is empty")
}

func TestReportTimingNilSafe(t *testing.T) {
	var reporter *TimingReporter // nil reporter

	// Should not panic when calling reportTiming on nil reporter
	require.NotPanics(t, func() {
		reporter.reportTiming("test_event", 100*time.Millisecond)
	}, "reportTiming should be nil-safe and not panic")

	// Should not panic when calling Close on nil reporter
	require.NotPanics(t, func() {
		reporter.Close()
	}, "Close should be nil-safe and not panic")
}

func TestReportTimingSendsEvent(t *testing.T) {
	conf := &config.Config{
		EventTimingNotifyCommand: "true", // command that always succeeds
		EventTimingNotifyArgs:    []string{"{event}", "{duration_ms}"},
	}
	logger := slog.Default()

	reporter := newTimingReporter(conf, logger)
	require.NotNil(t, reporter)
	defer reporter.Close()

	// Send an event
	reporter.reportTiming("test_event", 150*time.Millisecond)

	// Give worker time to process the event
	time.Sleep(100 * time.Millisecond)

	// If we reach here without panic or deadlock, the test passes
}

func TestReportTimingChannelFull(t *testing.T) {
	conf := &config.Config{
		EventTimingNotifyCommand: "sleep",
		EventTimingNotifyArgs:    []string{"10"}, // slow command to keep worker busy
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	reporter := newTimingReporter(conf, logger)
	require.NotNil(t, reporter)
	defer reporter.Close()

	// Fill the channel with 64 events (the buffer size)
	for i := 0; i < 64; i++ {
		reporter.reportTiming("fill_event", time.Duration(i)*time.Millisecond)
	}

	// Try to send one more event - it should not block (will be dropped)
	done := make(chan bool, 1)
	go func() {
		reporter.reportTiming("overflow_event", 999*time.Millisecond)
		done <- true
	}()

	// Wait with timeout to ensure it doesn't block
	select {
	case <-done:
		// Success - the call returned immediately (event was dropped)
	case <-time.After(1 * time.Second):
		t.Fatal("reportTiming blocked when channel was full - should have dropped the event")
	}
}
