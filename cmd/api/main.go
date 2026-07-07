package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	httpapi "github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/handler/http"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/platform/config"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/platform/database"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/platform/logging"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/sumiyapuletipalli/wallet-transfer-assignment/internal/service"
)

func main() {
	logger := logging.New()
	if err := run(logger); err != nil {
		logger.Error("application stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.OpenPostgres(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	unitOfWork := postgres.NewUnitOfWork(pool)
	transferService := service.NewTransferService(unitOfWork)
	handler := httpapi.NewHandler(transferService, logger)

	server := &http.Server{
		Addr:         cfg.HTTPAddress,
		Handler:      handler.Routes(),
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
		IdleTimeout:  cfg.IdleTimeout,
	}

	serverError := make(chan error, 1)
	go func() {
		logger.Info("HTTP server started", "address", cfg.HTTPAddress)
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		logger.Info("shutdown requested")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
	}
	return nil
}
