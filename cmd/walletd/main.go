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

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/httpapi"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(logger); err != nil {
		logger.Error("walletd exited", slog.Any("error", err))
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	dsn := env("DATABASE_URL", "sqlite://wallet.db")
	db, err := database.Open(ctx, dsn)
	if err != nil {
		return err
	}
	defer func() { _ = db.Close() }()

	if err := database.Migrate(ctx, db); err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              ":" + env("PORT", "8080"),
		Handler:           httpapi.NewServer(service.New(repository.NewStore(db)), logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errc := make(chan error, 1)
	go func() {
		logger.Info("listening", slog.String("addr", srv.Addr), slog.String("driver", string(db.Driver())))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errc <- err
		}
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
