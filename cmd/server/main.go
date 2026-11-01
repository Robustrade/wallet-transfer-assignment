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

	"wallet-transfer-assignment/internal/config"
	"wallet-transfer-assignment/internal/httpapi"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"
	"wallet-transfer-assignment/migrations"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 10 * time.Second
	// requestTimeout is shorter than writeTimeout. The socket timeout does not
	// cancel r.Context(), so a transfer waiting on a wallet lock would keep its
	// database connection and could commit after the client gave up.
	requestTimeout = 8 * time.Second
	idleTimeout    = 60 * time.Second
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	if err := run(); err != nil {
		slog.Error("server failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		return err
	}
	if err := migrations.Apply(ctx, pool); err != nil {
		return err
	}

	api := service.New(postgres.New(pool))
	server := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           httpapi.WithTimeout(httpapi.New(api), requestTimeout),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		return shutdownServer(server, 5*time.Second)
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

// shutdownServer waits for in-flight handlers, then closes them if they are
// still running. Shutdown leaves those connections open when its deadline
// passes. Close cancels their request contexts so the database work rolls
// back before the pool is closed.
func shutdownServer(server *http.Server, timeout time.Duration) error {
	shutdownCtx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		_ = server.Close()
		return err
	}
	return nil
}
