package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"wallet-transfer/internal/config"
	apphttp "wallet-transfer/internal/http"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
)

func main() {
	cfg := config.Load()

	ctx := context.Background()
	db, err := postgres.Open(ctx, cfg.DatabaseURL, postgres.PoolConfig{
		MaxOpenConns:    25,
		MaxIdleConns:    20,
		ConnMaxLifetime: 30 * time.Minute,
	})
	if err != nil {
		slog.Error("database connection failed", "error", err)
		os.Exit(1)
	}
	defer func() { _ = db.Close() }()

	if _, err := postgres.Migrate(ctx, db, cfg.MigrationsDir); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}

	txManager := postgres.NewTxManager(db)
	wallets := postgres.NewWalletRepository(txManager)
	transfers := postgres.NewTransferRepository(txManager)
	ledger := postgres.NewLedgerRepository(txManager)
	idempotency := postgres.NewIdempotencyRepository(txManager)

	transferService := service.NewTransferService(txManager, wallets, transfers, ledger, idempotency)
	router := apphttp.NewRouter(transferService)

	slog.Info("server listening", "addr", cfg.HTTPAddr)
	if err := http.ListenAndServe(cfg.HTTPAddr, router); err != nil {
		slog.Error("http server failed", "error", err)
		os.Exit(1)
	}
}
