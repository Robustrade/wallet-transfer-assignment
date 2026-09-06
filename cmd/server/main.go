package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"wallet-transfer/internal/database"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	dsn := envOr("DATABASE_URL", "postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable")
	migrationsDir := envOr("MIGRATIONS_DIR", "migrations")
	addr := envOr("ADDR", ":8080")

	db, err := database.Connect(dsn)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer db.Close()
	logger.Info("connected to database")

	if err := database.RunMigrations(db, migrationsDir); err != nil {
		logger.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	transferRepo := repository.NewPostgresTransferRepository(db)
	walletRepo := repository.NewPostgresWalletRepository(db)
	transferSvc := service.NewTransferService(transferRepo, logger)

	transferHandler := handler.NewTransferHandler(transferSvc)
	walletHandler := handler.NewWalletHandler(walletRepo)
	router := handler.NewRouter(transferHandler, walletHandler)

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info("starting server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		logger.Error("forced shutdown", "error", err)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
