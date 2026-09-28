// Command server runs the wallet transfer HTTP API.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/config"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/handler"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// shutdownTimeout bounds how long in-flight requests get to finish after
// SIGINT/SIGTERM.
const shutdownTimeout = 10 * time.Second

func main() {
	// Cancelled on SIGINT (Ctrl+C) or SIGTERM (docker stop, Kubernetes).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.LookupEnv, os.Stdout); err != nil {
		slog.New(slog.NewJSONHandler(os.Stderr, nil)).Error("server exited", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// run wires everything together and serves until ctx is cancelled. It is
// separate from main, so tests can start and stop a real server in-process.
func run(ctx context.Context, lookup func(string) (string, bool), logOut io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(logOut, nil))

	cfg, err := config.Load(lookup)
	if err != nil {
		return err
	}

	store, err := repository.Open(ctx, repository.Options{
		Driver:       cfg.DBDriver,
		DSN:          cfg.DBDSN,
		LockTimeout:  cfg.LockTimeout,
		SeedDemoData: true, // demo wallets for `make run` / `make demo`
	})
	if err != nil {
		return err
	}
	defer store.Close()

	gin.SetMode(gin.ReleaseMode)
	router := handler.NewRouter(handler.RouterConfig{
		Transfers:      service.NewTransferService(store.TxManager),
		Wallets:        service.NewWalletService(store.Repos),
		DB:             store.DB,
		Logger:         logger,
		RequestTimeout: cfg.RequestTimeout,
		MaxBodyBytes:   cfg.MaxBodyBytes,
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second, // slowloris protection
		// Longer than REQUEST_TIMEOUT, so the handler (not the server) decides
		// when a request has taken too long, and can answer with a JSON 503.
		WriteTimeout: cfg.RequestTimeout + 5*time.Second,
		IdleTimeout:  60 * time.Second,
	}

	serveErr := make(chan error, 1)
	go func() {
		// The DSN is deliberately not logged: it may contain a password.
		logger.Info("server listening", slog.String("addr", srv.Addr), slog.String("db_driver", cfg.DBDriver))
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
		}
		close(serveErr)
	}()

	select {
	case err := <-serveErr:
		return fmt.Errorf("listen: %w", err) // e.g. port already in use
	case <-ctx.Done():
	}

	// Stop accepting new connections and let in-flight requests finish. Each
	// one is a single DB transaction that either commits or rolls back, so
	// being cut off by the timeout cannot leave a partial transfer.
	logger.Info("shutting down", slog.String("timeout", shutdownTimeout.String()))
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	logger.Info("server stopped")
	return nil
}
