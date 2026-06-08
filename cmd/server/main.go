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

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/benjamin-benny/wallet-transfer/internal/handler"
	"github.com/benjamin-benny/wallet-transfer/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Default().Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run() error {
	log := slog.Default()

	dsn := envOrDefault("DATABASE_URL", "postgres://wallet:wallet@localhost:5432/wallet_transfer?sslmode=disable")
	addr := envOrDefault("ADDR", ":8080")
	migrationsPath := envOrDefault("MIGRATIONS_PATH", "migrations")

	// Run migrations before accepting traffic.
	if err := runMigrations(migrationsPath, dsn); err != nil {
		return fmt.Errorf("migrations: %w", err)
	}

	// Root context cancelled on SIGINT/SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Connect pool.
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("db pool: %w", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return fmt.Errorf("db ping: %w", err)
	}

	// Wire up layers.
	svc := service.NewTransferService(pool)
	transferHandler := handler.NewTransferHandler(svc)
	healthHandler := handler.NewHealthHandler(pool)

	// Routes — Go 1.22 method+path routing.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transferHandler.Create)
	mux.HandleFunc("GET /healthz", healthHandler.Check)

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadTimeout:       15 * time.Second,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Run server in a goroutine so we can listen for shutdown signals in main flow.
	serverErr := make(chan error, 1)
	go func() {
		log.Info("starting server", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serverErr <- err
			return
		}
		serverErr <- nil
	}()

	// Block until either: server crashes, or shutdown signal received.
	select {
	case err := <-serverErr:
		return err
	case <-ctx.Done():
		log.Info("shutdown signal received, draining connections")
	}

	// Graceful shutdown: stop accepting new connections, let in-flight requests
	// finish within the timeout. Critical for a transfer service — we don't
	// want to kill a request mid-DB-transaction.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}

	log.Info("server stopped cleanly")
	return nil
}

func runMigrations(migrationsPath, dsn string) error {
	log := slog.Default()

	m, err := migrate.New("file://"+migrationsPath, dsn)
	if err != nil {
		return err
	}
	defer func() {
		if srcErr, dbErr := m.Close(); srcErr != nil || dbErr != nil {
			log.Warn("migrate close failed", "source_err", srcErr, "db_err", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return err
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
