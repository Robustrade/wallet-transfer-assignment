package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, nil))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, logger)
	stop()
	if err != nil {
		// Underlying driver errors can contain connection credentials. Log only
		// the safe category; wrapped causes remain available to callers/tests.
		logger.Error("server stopped", "category", errorCategory(err))
		os.Exit(1)
	}
}
