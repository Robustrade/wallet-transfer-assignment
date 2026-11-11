package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
	appErrors "wallet-transfer-assignment/internal/appErrors"

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
)

func run(ctx context.Context, logger *slog.Logger) error {
	cfg, err := config.Get()
	if err != nil {
		return err
	}

	db, err := postgres.Init(ctx, cfg.DatabaseURL, cfg.OperationTimeout)
	if err != nil {
		return err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("%w: %w", appErrors.ErrDatabaseOpen, err)
	}

	defer func() {
		if closeErr := sqlDB.Close(); closeErr != nil {
			logger.Error("database pool close failed")
		}
	}()

	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", cfg.Address())
	if err != nil {
		return fmt.Errorf("%w: %w", appErrors.ErrListen, err)
	}

	transfers := service.NewTransferService(db, cfg.OperationTimeout, cfg.MaxTransferAmount)
	server := &http.Server{
		Handler:           handler.NewRouter(transfers, cfg.MaxTransferAmount),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	logger.Info("server listening", "address", listener.Addr().String())
	return serve(ctx, server, listener, cfg.ShutdownTimeout)
}

func errorCategory(err error) string {
	for _, category := range []error{
		appErrors.ErrDatabaseURLRequired,
		appErrors.ErrInvalidPort,
		appErrors.ErrInvalidShutdownTimeout,
		appErrors.ErrInvalidOperationTimeout,
		appErrors.ErrInvalidMaxTransferAmount,
		appErrors.ErrDatabaseOpen,
		appErrors.ErrDatabaseConnection,
		appErrors.ErrSchemaUnavailable,
		appErrors.ErrSchemaIncompatible,
		appErrors.ErrListen,
		appErrors.ErrServe,
		appErrors.ErrShutdown,
	} {
		if errors.Is(err, category) {
			return category.Error()
		}
	}
	return "unexpected startup failure"
}
