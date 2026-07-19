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

	"wallet-transfer-assignment/internal/api"
	"wallet-transfer-assignment/internal/infra"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	config := infra.LoadConfig()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.Connect(ctx, config.DatabaseURL)
	if err != nil {
		logger.Error("connect_database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	store := postgres.NewStore(pool)
	server := api.NewServer(
		service.NewTransferService(store),
		service.NewWalletService(store),
		logger,
	)

	httpServer := &http.Server{
		Addr:              ":" + config.Port,
		Handler:           server.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("server_started", "port", config.Port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("server_failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("server_shutdown_failed", "error", err)
		os.Exit(1)
	}
	logger.Info("server_stopped")
}
