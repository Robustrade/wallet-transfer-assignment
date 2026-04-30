package infra

import (
	"log/slog"
	"os"
	"strings"
)

// LoggerConfig holds logger options.
type LoggerConfig struct {
	// Level is one of "debug", "info", "warn", "error" (case-insensitive).
	// Empty defaults to "info".
	Level string
	// JSON, if true, emits structured JSON log lines; otherwise text.
	JSON bool
}

// InitLogger constructs a *slog.Logger writing to stdout.
func InitLogger(cfg LoggerConfig) *slog.Logger {
	level := parseLevel(cfg.Level)
	opts := &slog.HandlerOptions{Level: level}

	var handler slog.Handler
	if cfg.JSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}
	return slog.New(handler)
}

func parseLevel(s string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return slog.LevelDebug
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
