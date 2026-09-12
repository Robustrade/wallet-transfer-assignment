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

	"github.com/heisenberglit/wallet-transfer-assignment/internal/db"
	httphandler "github.com/heisenberglit/wallet-transfer-assignment/internal/handler/http"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/heisenberglit/wallet-transfer-assignment/internal/service"
)

const shutdownTimeout = 10 * time.Second

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	ctx := context.Background()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable"
	}

	pool, err := db.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect to database: %w", err)
	}
	defer pool.Close()

	wallets := postgres.NewWalletRepository(pool)
	transfers := postgres.NewTransferRepository(pool)
	executor := postgres.NewTransferExecutor(pool)

	transferService := service.NewTransferService(wallets, transfers, executor)
	transferHandler := httphandler.NewTransferHandler(transferService)
	router := httphandler.NewRouter(transferHandler)

	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	notifyCtx, stopNotify := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopNotify()

	select {
	case err := <-serverErr:
		return err
	case <-notifyCtx.Done():
		slog.Info("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()

		if err := srv.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("graceful shutdown: %w", err)
		}
	}

	return nil
}
