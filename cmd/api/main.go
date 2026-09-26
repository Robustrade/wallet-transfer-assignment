// Command api runs the wallet transfer HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"

	"wallet/internal/api"
	"wallet/internal/config"
	"wallet/internal/constants"
	"wallet/internal/service"
	"wallet/internal/store/postgres"
	"wallet/migrations"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	slog.SetDefault(logger)

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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Migrations are an explicit operator step by default (cmd/migrate);
	// RUN_MIGRATIONS=true is for single-instance deployments and local runs.
	if cfg.RunMigrations {
		if err := applyMigrations(ctx, logger, cfg.DatabaseURL); err != nil {
			return err
		}
	}

	store, err := postgres.New(ctx, cfg.DatabaseURL, constants.MaxConns, cfg.LockTimeoutMS)
	if err != nil {
		return err
	}
	defer store.Close()

	svc := service.New(store, logger)
	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           api.NewRouter(svc, logger, store.Ping),
		ReadHeaderTimeout: constants.ReadHeaderTimeout,
		ReadTimeout:       constants.ReadTimeout,
		WriteTimeout:      constants.WriteTimeout,
		IdleTimeout:       constants.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		logger.Info("shutting down")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), constants.ShutdownTimeout)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}

// applyMigrations applies embedded migrations over a short-lived connection,
// then releases it: the API's pool must not be the migration client. The
// connect itself is bounded so startup fails fast on an unreachable database.
func applyMigrations(ctx context.Context, logger *slog.Logger, databaseURL string) error {
	connectCtx, cancel := context.WithTimeout(ctx, constants.ConnectTimeout)
	conn, err := pgx.Connect(connectCtx, databaseURL)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close(context.WithoutCancel(ctx))

	applied, err := migrations.Apply(ctx, conn)
	if err != nil {
		return err
	}
	logger.Info("migrations applied", "versions", applied)
	return nil
}
