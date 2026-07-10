// Package logger provides a single zerolog-backed logger for the app,
// configured once at startup with a runtime-selectable level.
package logger

import (
	"os"
	"strings"
	"sync"

	"github.com/rs/zerolog"
)

var (
	mu  sync.RWMutex
	log = newLogger(zerolog.InfoLevel)
)

func newLogger(level zerolog.Level) zerolog.Logger {
	return zerolog.New(os.Stdout).Level(level).With().Timestamp().Caller().Logger()
}

// Init (re)configures the global logger with the given level string
// (e.g. "debug", "info", "warn", "error"). Unrecognized or empty values
// fall back to "info" rather than failing startup over a bad env var.
func Init(level string) {
	parsed, err := zerolog.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	if err != nil {
		parsed = zerolog.InfoLevel
	}

	mu.Lock()
	defer mu.Unlock()
	log = newLogger(parsed)
}

// L returns the current global logger.
func L() *zerolog.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return &log
}

func Debug() *zerolog.Event { return L().Debug() }
func Info() *zerolog.Event  { return L().Info() }
func Warn() *zerolog.Event  { return L().Warn() }
func Error() *zerolog.Event { return L().Error() }
func Fatal() *zerolog.Event { return L().Fatal() }
