// Command server runs the wallet transfer HTTP service.
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

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/config"
	httphandler "github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/handler/http"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/platform/id"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/platform/logger"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/migrations"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBConnTimeout)
	if err != nil {
		return err
	}
	defer db.Close()

	if cfg.AutoMigrate {
		if err := postgres.NewMigrator(db, migrations.FS).Up(ctx); err != nil {
			return err
		}
		log.Info("database migrations applied")
	}

	repos := service.Repositories{
		Wallets:     postgres.NewWalletRepository(db),
		Transfers:   postgres.NewTransferRepository(db),
		Ledger:      postgres.NewLedgerRepository(db),
		Idempotency: postgres.NewIdempotencyRepository(db),
	}
	transferSvc := service.NewTransferService(repos, db, id.NewGenerator("txn", 16), log)
	walletSvc := service.NewWalletService(repos.Wallets)

	router := httphandler.NewRouter(httphandler.Deps{
		Transfers: transferSvc,
		Wallets:   walletSvc,
		DB:        db,
		Logger:    log,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case serveErr := <-errCh:
		return serveErr
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
