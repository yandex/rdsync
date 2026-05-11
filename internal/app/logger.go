package app

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/diode"
)

func parseLevel(level string) (zerolog.Level, error) {
	switch level {
	case "Debug":
		return zerolog.DebugLevel, nil
	case "Info":
		return zerolog.InfoLevel, nil
	case "Warn":
		return zerolog.WarnLevel, nil
	case "Error":
		return zerolog.ErrorLevel, nil
	}
	return zerolog.InfoLevel, fmt.Errorf("unknown log level: %s", level)
}

func levelToUpper(i interface{}) string {
	if i == nil {
		return ""
	}
	return strings.ToUpper(fmt.Sprintf("%-5s", i))
}

func newMainLogger(level zerolog.Level, bufSize int, poll time.Duration) (*zerolog.Logger, io.Closer) {
	cw := zerolog.ConsoleWriter{
		Out:         os.Stderr,
		NoColor:     true,
		TimeFormat:  time.RFC3339,
		FormatLevel: levelToUpper,
	}
	dw := diode.NewWriter(cw, bufSize, poll, nil)
	l := zerolog.New(dw).Level(level).With().Timestamp().Logger()
	return &l, dw
}

func newEventLogger(f *os.File, bufSize int, poll time.Duration) (*zerolog.Logger, io.Closer) {
	cw := zerolog.ConsoleWriter{
		Out:         f,
		NoColor:     true,
		TimeFormat:  time.RFC3339,
		FieldsOrder: []string{"event", "duration_ms"},
		FormatTimestamp: func(i interface{}) string {
			return fmt.Sprintf("time=%v", i)
		},
		FormatLevel: func(i interface{}) string {
			return fmt.Sprintf("level=%s", strings.ToUpper(fmt.Sprintf("%s", i)))
		},
		FormatMessage: func(i interface{}) string {
			return fmt.Sprintf("msg=%v", i)
		},
		FormatFieldName: func(i interface{}) string {
			return fmt.Sprintf("%s=", i)
		},
		FormatFieldValue: func(i interface{}) string {
			return fmt.Sprintf("%v", i)
		},
	}
	dw := diode.NewWriter(cw, bufSize, poll, nil)
	l := zerolog.New(dw).Level(zerolog.InfoLevel).With().Timestamp().Logger()
	return &l, dw
}
