// Command server runs the wallet transfer HTTP API.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/db"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/handler"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/repository"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/service"
	"github.com/sagarsuperuser/wallet-transfer-assignment/migrations"
)

const (
	defaultAddr     = ":8080"
	readTimeout     = 10 * time.Second
	writeTimeout    = 15 * time.Second
	idleTimeout     = 60 * time.Second
	shutdownTimeout = 20 * time.Second
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("server stopped", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is not set")
	}

	// Interrupt and SIGTERM begin a graceful shutdown rather than dropping
	// in-flight transfers.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()

	// Migrating on boot keeps a single command runnable from a clean checkout.
	// It is safe with several instances starting at once: Migrate serializes
	// them with an advisory lock and records what it applied.
	if err := db.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}

	transfers := handler.NewTransfers(service.NewTransfers(repository.New(pool), logger), logger)

	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = defaultAddr
	}

	server := &http.Server{
		Addr:         addr,
		Handler:      transfers.Routes(),
		ReadTimeout:  readTimeout,
		WriteTimeout: writeTimeout,
		IdleTimeout:  idleTimeout,
	}

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", addr))
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
		}
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
	}

	// Let in-flight requests finish; a transfer mid-transaction should commit
	// or roll back on its own terms rather than having its connection cut.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	return server.Shutdown(shutdownCtx)
}
