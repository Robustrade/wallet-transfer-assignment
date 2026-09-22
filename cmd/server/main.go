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

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/config"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/migrations"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/repository/postgres"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/router"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := newLogger(cfg.LogLevel)
	slog.SetDefault(logger)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := migrations.Apply(ctx, pool); err != nil {
		return err
	}

	uow := postgres.NewUnitOfWork(pool, cfg.MaxClaimAttempts)
	transferService := service.NewTransferService(uow, service.WithLogger(logger))
	walletService := service.NewWalletService(uow, service.WithLogger(logger))

	handler := router.New(transferService, walletService, logger)
	tlsConfig, err := cfg.TLSConfig()
	if err != nil {
		return err
	}
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           handler,
		TLSConfig:         tlsConfig,
		ReadHeaderTimeout: 5 * time.Second,
	}

	serverErr := make(chan error, 1)
	go func() {
		serve := server.ListenAndServe
		protocol := "http"
		if cfg.SSLEnabled {
			serve = func() error {
				return server.ListenAndServeTLS(cfg.TLSCertFile, cfg.TLSKeyFile)
			}
			protocol = "https"
		}
		logger.Info("listening", "port", cfg.Port, "protocol", protocol)
		if err := serve(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-serverErr:
		if err != nil {
			return err
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return server.Shutdown(shutdownCtx)
}

func newLogger(level string) *slog.Logger {
	var logLevel slog.Level
	if err := logLevel.UnmarshalText([]byte(level)); err != nil {
		logLevel = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: logLevel}))
}
