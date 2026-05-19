// Command server is the composition root for the wallet transfer service.
// It loads config, wires the feature modules into the router and serves
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

	"github.com/hariharandr/wallet-transfer-assignment/internal/config"
	"github.com/hariharandr/wallet-transfer-assignment/internal/httpapi"
	"github.com/hariharandr/wallet-transfer-assignment/internal/platform/migrate"
	"github.com/hariharandr/wallet-transfer-assignment/internal/platform/postgres"
	"github.com/hariharandr/wallet-transfer-assignment/internal/transfer"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("server exited with error", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// migrate first, then open the runtime pool. if the schema can't
	// get we don't want to serve traffic.
	if err := migrate.Run(cfg.DatabaseURL); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	transfers := transfer.NewHandler(transfer.NewService(transfer.NewPgxRepository(pool)))
	router := httpapi.NewRouter(httpapi.Routes{Transfer: transfers.Routes})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("listen failed", "error", err)
			stop()
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received, draining")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}
