// Package logger builds the application's structured logger.
package logger

import (
	"log/slog"
	"os"
	"strings"
)

// New returns a JSON slog.Logger at the given level ("debug", "info", "warn",
// "error"; anything else defaults to info).
func New(level string) *slog.Logger {
	var lvl slog.Level
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		lvl = slog.LevelDebug
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		lvl = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl}))
}
