// Command wallet runs the wallet transfer service.
//
//	wallet serve              start the HTTP API
//	wallet migrate [--seed]   apply schema migrations (and idempotent demo seed data), then exit
//	wallet healthcheck        probe /readyz; exit 0 when ready (used by the container health check)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/app"
	"github.com/Robustrade/wallet-transfer-assignment/internal/platform/config"
	"github.com/Robustrade/wallet-transfer-assignment/internal/platform/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/platform/logging"
	"github.com/Robustrade/wallet-transfer-assignment/internal/platform/migrate"
	"github.com/Robustrade/wallet-transfer-assignment/migrations"
)

const usage = `usage: wallet <command>

commands:
  serve              start the HTTP API
  migrate [--seed]   apply schema migrations (and demo seed data), then exit
  healthcheck        exit 0 if the API reports ready`

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, usage)
		return 2
	}
	var err error
	switch args[0] {
	case "serve":
		err = serve()
	case "migrate":
		err = migrateCmd(args[1:])
	case "healthcheck":
		err = healthcheck()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s\n", args[0], usage)
		return 2
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallet %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

func serve() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(os.Stdout, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBConnectWait, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           app.NewHandler(pool, cfg.DBLockTimeout, logger),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", slog.String("addr", cfg.HTTPAddr))
		serveErr <- srv.ListenAndServe()
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
	}

	// Graceful shutdown: stop accepting connections and let in-flight transfers finish.
	logger.Info("shutting down", slog.Duration("timeout", cfg.ShutdownTimeout))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}
	logger.Info("shutdown complete")
	return nil
}

func migrateCmd(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	seed := fs.Bool("seed", false, "also apply idempotent demo seed data")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	logger := logging.New(os.Stdout, cfg.LogLevel)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Connect(ctx, cfg.DatabaseURL, 2, cfg.DBConnectWait, logger)
	if err != nil {
		return err
	}
	defer pool.Close()

	if _, err := migrate.Up(ctx, pool, migrations.FS, logger); err != nil {
		return err
	}
	if *seed {
		return migrate.Seed(ctx, pool, migrations.FS, logger)
	}
	return nil
}

func healthcheck() error {
	client := &http.Client{Timeout: 3 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, config.HealthcheckURL(), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("not ready: %s", resp.Status)
	}
	return nil
}
