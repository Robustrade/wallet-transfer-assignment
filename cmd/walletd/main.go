// Command walletd runs the wallet transfer HTTP service.
//
// On startup it loads configuration from the environment, connects to
// PostgreSQL, applies embedded schema migrations, and serves the HTTP API with
// graceful shutdown on SIGINT/SIGTERM.
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

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/config"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/handler"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/feroz-khatri/wallet-transfer-assignment/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("walletd terminated", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Bound startup work (connect + migrate) so a wedged database does not hang
	// the process indefinitely.
	startupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.NewDB(startupCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := db.Migrate(startupCtx); err != nil {
		return err
	}
	logger.Info("migrations applied")

	svc := service.New(service.Repositories{
		Wallets:     postgres.NewWalletRepository(db),
		Transfers:   postgres.NewTransferRepository(db),
		Ledger:      postgres.NewLedgerRepository(db),
		Idempotency: postgres.NewIdempotencyRepository(db),
		TxManager:   db,
	})

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      handler.New(svc),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	// Run the server until a termination signal arrives, then shut down
	// gracefully within the configured budget.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	serverErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}
