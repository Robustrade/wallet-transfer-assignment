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

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	// Configure structured logger
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	slog.Info("Starting Wallet Transfer Service...")

	cfg, err := config.Load()
	if err != nil {
		slog.Error("Failed loading configuration", "error", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db, err := database.NewPostgresPool(ctx, cfg)
	if err != nil {
		slog.Error("Failed connecting to PostgreSQL pool", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	// Initialize repositories
	transactor := postgres.NewTransactor(db.Pool)
	walletRepo := postgres.NewWalletRepository(db.Pool)
	transferRepo := postgres.NewTransferRepository(db.Pool)
	ledgerRepo := postgres.NewLedgerRepository(db.Pool)
	idempotencyRepo := postgres.NewIdempotencyRepository(db.Pool)

	// Initialize services
	walletService := service.NewWalletService(walletRepo, ledgerRepo)
	transferService := service.NewTransferService(transactor, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Initialize handlers
	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(walletService)
	healthHandler := handler.NewHealthHandler(db)

	// Wire HTTP Router
	router := handler.NewRouter(transferHandler, walletHandler, healthHandler)

	srv := &http.Server{
		Addr:         ":" + cfg.Port,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	// Start HTTP server in a separate goroutine
	go func() {
		slog.Info("Server listening for incoming HTTP requests", "port", cfg.Port, "env", cfg.Environment)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("HTTP server failed unexpectedly", "error", err)
			os.Exit(1)
		}
	}()

	// Listen for shutdown OS signals
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("Shutting down server gracefully...")

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer shutdownCancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("Server forced to shutdown", "error", err)
		os.Exit(1)
	}

	slog.Info("Server exited cleanly")
}
