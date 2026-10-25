// Command server runs the wallet transfer HTTP API.
//
// Configuration (environment variables):
//
//	DATABASE_URL  PostgreSQL connection string (required)
//	HTTP_ADDR     listen address (default ":8080")
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GaureshPatil/wallet-transfer-assignment/internal/handler"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/GaureshPatil/wallet-transfer-assignment/internal/service"
	"github.com/GaureshPatil/wallet-transfer-assignment/migrations"
)

// shutdownGracePeriod is how long in-flight requests may finish after SIGTERM
// before they are cancelled.
const shutdownGracePeriod = 15 * time.Second

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(log); err != nil {
		log.Error("server stopped", "error", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		return errors.New("DATABASE_URL is required")
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := pool.Ping(ctx); err != nil {
		return err
	}
	if err := postgres.Migrate(ctx, pool, migrations.FS); err != nil {
		return err
	}

	store := postgres.NewStore(pool)
	srv := &http.Server{
		Handler:           handler.New(service.NewTransferService(store), service.NewWalletService(store), log),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	log.Info("listening", "addr", ln.Addr().String())
	return serve(ctx, srv, ln, shutdownGracePeriod, log)
}
