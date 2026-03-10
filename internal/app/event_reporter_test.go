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
		EventTimingLogFile: "", // empty path
	}
	logger := slog.Default()

	reporter := newTimingReporter(conf, logger)
	require.Nil(t, reporter, "newTimingReporter should return nil when EventTimingLogFile is empty")
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

func TestReportTimingWritesToFile(t *testing.T) {
	tmpFile, err := os.CreateTemp("", "rdsync_timing_test_*.log")
	require.NoError(t, err)
	tmpPath := tmpFile.Name()
	tmpFile.Close()
	defer os.Remove(tmpPath)

	conf := &config.Config{
		EventTimingLogFile: tmpPath,
	}
	logger := slog.Default()

	reporter := newTimingReporter(conf, logger)
	require.NotNil(t, reporter)

	reporter.reportTiming("switchover_complete", 1523*time.Millisecond)
	reporter.reportTiming("failover_complete", 45002*time.Millisecond)
	reporter.Close()

	content, err := os.ReadFile(tmpPath)
	require.NoError(t, err)

	output := string(content)
	require.Contains(t, output, "event=switchover_complete")
	require.Contains(t, output, "duration_ms=1523")
	require.Contains(t, output, "event=failover_complete")
	require.Contains(t, output, "duration_ms=45002")
	require.Contains(t, output, "msg=event_timing")
}

func TestNewTimingReporterInvalidPath(t *testing.T) {
	conf := &config.Config{
		EventTimingLogFile: "/nonexistent/directory/timing.log",
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))

	reporter := newTimingReporter(conf, logger)
	require.Nil(t, reporter, "newTimingReporter should return nil when log file cannot be opened")
}

func TestReportTimingReopen(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "rdsync_reopen_test_*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	logPath := tmpDir + "/events.log"

	conf := &config.Config{
		EventTimingLogFile: logPath,
	}
	logger := slog.Default()

	reporter := newTimingReporter(conf, logger)
	require.NotNil(t, reporter)
	defer reporter.Close()

	// Write first event
	reporter.reportTiming("switchover_complete", 1000*time.Millisecond)

	// Simulate logrotate: rename the file
	rotatedPath := logPath + ".1"
	err = os.Rename(logPath, rotatedPath)
	require.NoError(t, err)

	// Reopen the log (creates a new file at the original path)
	reporter.Reopen()

	// Write second event
	reporter.reportTiming("failover_complete", 2000*time.Millisecond)

	// Verify rotated file contains only the first event
	rotatedContent, err := os.ReadFile(rotatedPath)
	require.NoError(t, err)
	require.Contains(t, string(rotatedContent), "event=switchover_complete")
	require.Contains(t, string(rotatedContent), "duration_ms=1000")
	require.NotContains(t, string(rotatedContent), "event=failover_complete")

	// Verify new file contains only the second event
	newContent, err := os.ReadFile(logPath)
	require.NoError(t, err)
	require.Contains(t, string(newContent), "event=failover_complete")
	require.Contains(t, string(newContent), "duration_ms=2000")
	require.NotContains(t, string(newContent), "event=switchover_complete")
}

func TestReopenNilSafe(t *testing.T) {
	var reporter *TimingReporter
	require.NotPanics(t, func() {
		reporter.Reopen()
	}, "Reopen should be nil-safe and not panic")
}
