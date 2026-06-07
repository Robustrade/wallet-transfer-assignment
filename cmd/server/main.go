// Command server is the entrypoint for the wallet transfer HTTP API.
//
// It wires configuration, the persistence store (PostgreSQL when DATABASE_URL
// is set, otherwise an in-memory store for local demos), the service layer and
// the HTTP transport, then serves with graceful shutdown.
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

	"github.com/Robustrade/wallet-transfer-assignment/internal/config"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/memory"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	httptransport "github.com/Robustrade/wallet-transfer-assignment/internal/transport/http"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	if err := run(logger); err != nil {
		logger.Error("server exited with error", slog.String("error", err.Error()))
		os.Exit(1)
	}
}

// run performs startup, serves until a termination signal, then shuts down
// gracefully. Returning an error lets main own the exit code.
func run(logger *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// Root context cancelled on SIGINT/SIGTERM for coordinated shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	store, err := newStore(ctx, cfg, logger)
	if err != nil {
		return err
	}
	defer store.Close()

	// Wire the layers: store -> services -> handler -> router.
	transferSvc := service.NewTransferService(store, nil, logger)
	walletSvc := service.NewWalletService(store, nil, logger)
	handler := httptransport.NewHandler(transferSvc, walletSvc, logger)
	router := httptransport.NewRouter(handler)

	srv := &http.Server{
		Addr:         cfg.HTTPAddr,
		Handler:      router,
		ReadTimeout:  cfg.ReadTimeout,
		WriteTimeout: cfg.WriteTimeout,
	}

	// Serve in the background so we can wait on the shutdown signal.
	serveErr := make(chan error, 1)
	go func() {
		logger.Info("server listening", slog.String("addr", cfg.HTTPAddr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return err
	}
	logger.Info("server stopped cleanly")
	return nil
}

// newStore selects and initialises the persistence store based on config.
func newStore(ctx context.Context, cfg config.Config, logger *slog.Logger) (repository.Store, error) {
	if cfg.UsesInMemoryStore() {
		logger.Warn("DATABASE_URL not set: using in-memory store (data is not persisted)")
		return memory.NewStore(), nil
	}

	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pg, err := postgres.New(connectCtx, postgres.Config{
		DSN:      cfg.DatabaseURL,
		MaxConns: cfg.DBMaxConns,
	})
	if err != nil {
		return nil, err
	}
	if err := pg.Migrate(connectCtx); err != nil {
		pg.Close()
		return nil, err
	}
	logger.Info("connected to postgres", slog.Int("max_conns", int(cfg.DBMaxConns)))
	return pg, nil
}
