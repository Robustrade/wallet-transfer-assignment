package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"
	appErrors "wallet-transfer-assignment/internal/appErrors"
)

func serve(ctx context.Context, server *http.Server, listener net.Listener, grace time.Duration) error {
	done := make(chan error, 1)
	go func() {
		done <- server.Serve(listener)
	}()

	select {
	case err := <-done:
		_ = server.Close()
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return fmt.Errorf("%w: %w", appErrors.ErrServe, err)
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), grace)
		defer cancel()

		if err := server.Shutdown(shutdownCtx); err != nil {
			_ = server.Close() // Cancels remaining requests after the grace budget.
			if serveErr := <-done; serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
				return fmt.Errorf("%w: %w", appErrors.ErrShutdown, errors.Join(err, serveErr))
			}
			return fmt.Errorf("%w: %w", appErrors.ErrShutdown, err)
		}

		if err := <-done; !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("%w: %w", appErrors.ErrServe, err)
		}
		return nil
	}
}
