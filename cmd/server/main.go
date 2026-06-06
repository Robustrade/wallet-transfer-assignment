// Package main is the entrypoint for the wallet transfer service.
// It wires all dependencies, runs database migrations, and starts the HTTP server.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	migrations "github.com/Robustrade/wallet-transfer-assignment/migrations"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// ── Configuration ────────────────────────────────────────────────────────
	databaseURL := envOrDefault("DATABASE_URL", "postgres://wallet:wallet@localhost:5432/wallet_db?sslmode=disable")
	port := envOrDefault("PORT", "8080")

	// Handle `migrate` sub-command for manual migration runs.
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		return db.Migrate(databaseURL, migrations.FS, ".")
	}

	// ── Database ─────────────────────────────────────────────────────────────
	slog.Info("running database migrations", "layer", "main")

	if err := db.Migrate(databaseURL, migrations.FS, "."); err != nil {
		return fmt.Errorf("main: migrate: %w", err)
	}

	pool, err := db.NewPool(ctx, databaseURL)
	if err != nil {
		return fmt.Errorf("main: connect db: %w", err)
	}

	defer pool.Close()

	// ── Dependencies ─────────────────────────────────────────────────────────
	log := slog.Default()
	walletRepo := repository.NewWalletRepo(log)
	transferRepo := repository.NewTransferRepo(log)
	ledgerRepo := repository.NewLedgerRepo(log)
	idemRepo := repository.NewIdempotencyRepo(log)

	svc := service.NewTransferService(
		pool,
		walletRepo,
		transferRepo,
		ledgerRepo,
		idemRepo,
		slog.Default(),
	)

	// ── HTTP ─────────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	h := handler.New(svc)
	h.Register(r)

	srv := &http.Server{
		Addr:         ":" + port,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		slog.Info("shutting down server", "layer", "main")

		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()

		if shutErr := srv.Shutdown(shutdownCtx); shutErr != nil {
			slog.Error("graceful shutdown failed", "layer", "main", "error", shutErr)
		}
	}()

	slog.Info("starting server", "layer", "main", "port", port)

	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("main: listen: %w", err)
	}

	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}

	return fallback
}
