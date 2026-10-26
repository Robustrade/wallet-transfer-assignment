package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"

	"wallettransfer/internal/handler"
	"wallettransfer/internal/repository"
	"wallettransfer/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("wallet server stopped", "error", err)
		os.Exit(1)
	}
}

func run() error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL must point to a migrated PostgreSQL database")
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(30 * time.Minute)

	pingCtx, cancelPing := context.WithTimeout(context.Background(), 5*time.Second)
	if err := db.PingContext(pingCtx); err != nil {
		cancelPing()
		return fmt.Errorf("connect to database: %w", err)
	}
	// Refuse to accept transfers against an uninitialized database. The
	// migration is an explicit deployment step, not a startup side effect.
	var schemaReady bool
	err = db.QueryRowContext(pingCtx, `SELECT to_regclass('wallets') IS NOT NULL
		AND to_regclass('transfers') IS NOT NULL
		AND to_regclass('ledger_entries') IS NOT NULL`).Scan(&schemaReady)
	cancelPing()
	if err != nil {
		return fmt.Errorf("inspect database schema: %w", err)
	}
	if !schemaReady {
		return errors.New("database schema is missing; apply migrations/000001_init.up.sql before starting")
	}

	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}

	server := &http.Server{
		Handler:           handler.New(service.New(repository.NewPostgres(db))),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serveErr := make(chan error, 1)
	go func() { serveErr <- server.Serve(listener) }()
	slog.Info("wallet transfer server listening", "address", listener.Addr().String())

	select {
	case err := <-serveErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
	case <-ctx.Done():
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return fmt.Errorf("shutdown HTTP: %w", err)
	}
	slog.Info("wallet transfer server stopped")
	return nil
}
