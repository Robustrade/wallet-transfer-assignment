// Command server runs the wallet transfer API.
//
// Configuration comes from the environment: DATABASE_URL (required) and
// HTTP_ADDR (default ":8080").
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/pravinkanna/wallet-transfer-assignment/internal/handler"
	"github.com/pravinkanna/wallet-transfer-assignment/internal/repository"
	"github.com/pravinkanna/wallet-transfer-assignment/internal/service"
)

// shutdownTimeout is how long in-flight requests get to finish on shutdown.
const shutdownTimeout = 10 * time.Second

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("create pool: %w", err)
	}
	defer pool.Close()

	if err := repository.ApplySchema(ctx, pool); err != nil {
		return err
	}
	if err := repository.ApplySeed(ctx, pool); err != nil {
		return err
	}

	server := &http.Server{
		Handler:           handler.New(service.New(repository.New(pool)), logger),
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       5 * time.Second,
		// Longer than the 5 s request deadline, so an error response can
		// still be written (design §9).
		WriteTimeout: 10 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}
	logger.Info("server listening", "addr", listener.Addr().String())

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	logger.Info("server shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shut down: %w", err)
	}
	return nil
}
