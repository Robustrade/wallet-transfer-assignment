package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// serve runs srv on ln until ctx is cancelled, then shuts down gracefully,
// letting in-flight requests finish for up to grace.
//
// If they do not finish in time, the context of every active request is
// cancelled and all connections are closed. That matters because a handler
// blocked on a database row lock only returns once its context is cancelled;
// without it the handler would keep its pooled connection, pool.Close() would
// wait for it, and the process could never exit. A forced shutdown is returned
// as an error.
func serve(ctx context.Context, srv *http.Server, ln net.Listener, grace time.Duration, log *slog.Logger) error {
	requests, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	srv.BaseContext = func(net.Listener) context.Context { return requests }

	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()

	select {
	case err := <-served:
		return err // Serve only returns early on a real failure
	case <-ctx.Done():
	}

	log.Info("shutting down", "grace", grace.String())
	shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	err := srv.Shutdown(shutdownCtx)
	if err == nil {
		return nil
	}

	log.Warn("graceful shutdown timed out; cancelling active requests", "error", err)
	cancelRequests()
	closeErr := srv.Close()
	return errors.Join(fmt.Errorf("graceful shutdown: %w", err), closeErr)
}
