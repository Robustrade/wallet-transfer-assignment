package service_test

import (
	"context"
	"io"
	"log/slog"
)

// discardLogger returns a logger that drops all output, keeping test logs clean.
func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ctx is a convenience background context for tests.
func ctx() context.Context { return context.Background() }
