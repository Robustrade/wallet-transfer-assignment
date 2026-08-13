package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/robustrade/wallet-transfer-assignment/internal/config"
	"github.com/robustrade/wallet-transfer-assignment/internal/db"
	"github.com/robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/robustrade/wallet-transfer-assignment/internal/router"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

func main() {
	cfg := config.Load()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Connect(ctx, cfg.DatabaseURL)
	if err != nil {
		slog.Error("connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := db.Migrate(ctx, pool); err != nil {
		slog.Error("run migrations", "error", err)
		os.Exit(1)
	}

	walletRepo := postgres.NewWalletRepository(pool)
	transferRepo := postgres.NewTransferRepository(pool)
	ledgerRepo := postgres.NewLedgerRepository(pool)
	idempotencyRepo := postgres.NewIdempotencyRepository(pool)
	txManager := postgres.NewTxManager(pool)

	transferService := service.NewTransferService(txManager, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)
	walletService := service.NewWalletService(walletRepo, ledgerRepo)

	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(walletService)

	r := router.New(transferHandler, walletHandler)

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           r,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		slog.Info("server starting", "port", cfg.Port)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	slog.Info("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", "error", err)
	}
}
