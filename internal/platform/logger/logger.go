// Package logger builds the application's structured logger. It has no
// domain knowledge — see docs/DESIGN.md §8.
package logger

import (
	"log/slog"
	"os"
)

// New returns a JSON-structured slog.Logger writing to stdout.
func New() *slog.Logger {
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	return slog.New(handler)
}
