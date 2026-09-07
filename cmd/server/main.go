// Command server runs the wallet transfer HTTP service.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/config"
	httphandler "github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/handler/http"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/platform/id"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/platform/logger"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/memory"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/migrations"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

func main() {
	if err := run(); err != nil {
		slog.Error("server exited with error", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(cfg.LogLevel)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	repos, txm, pinger, cleanup, err := buildRepositories(ctx, cfg, log)
	if err != nil {
		return err
	}
	defer cleanup()

	transferSvc := service.NewTransferService(repos, txm, id.NewGenerator("txn", 16), log)
	walletSvc := service.NewWalletService(repos.Wallets)

	router := httphandler.NewRouter(httphandler.Deps{
		Transfers: transferSvc,
		Wallets:   walletSvc,
		DB:        pinger,
		Logger:    log,
	})

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("http server listening", "addr", cfg.HTTPAddr)
		if serveErr := srv.ListenAndServe(); serveErr != nil && !errors.Is(serveErr, http.ErrServerClosed) {
			errCh <- serveErr
		}
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown signal received")
	case serveErr := <-errCh:
		return serveErr
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// buildRepositories selects the persistence backend. REPO=memory runs entirely
// in process (no database) and is handy for local smoke testing; the default is
// PostgreSQL.
func buildRepositories(ctx context.Context, cfg config.Config, log *slog.Logger) (
	repos service.Repositories,
	txm service.TxManager,
	pinger httphandler.Pinger,
	cleanup func(),
	err error,
) {
	if strings.EqualFold(os.Getenv("REPO"), "memory") {
		log.Warn("using in-memory repository — data is not persisted")
		store := memory.New()
		seedMemory(store)
		noCleanup := func() { /* in-memory store holds no external resources */ }
		return service.Repositories{
			Wallets:     store,
			Transfers:   store,
			Ledger:      store,
			Idempotency: store,
		}, store, nil, noCleanup, nil
	}

	db, err := postgres.Connect(ctx, cfg.DatabaseURL, cfg.DBMaxConns, cfg.DBConnTimeout)
	if err != nil {
		return service.Repositories{}, nil, nil, nil, err
	}

	if cfg.AutoMigrate {
		if migErr := postgres.NewMigrator(db, migrations.FS).Up(ctx); migErr != nil {
			db.Close()
			return service.Repositories{}, nil, nil, nil, migErr
		}
		log.Info("database migrations applied")
	}

	return service.Repositories{
		Wallets:     postgres.NewWalletRepository(db),
		Transfers:   postgres.NewTransferRepository(db),
		Ledger:      postgres.NewLedgerRepository(db),
		Idempotency: postgres.NewIdempotencyRepository(db),
	}, db, db, db.Close, nil
}

func seedMemory(store *memory.Store) {
	store.SeedWallet("wallet_1", 100_000, "USD")
	store.SeedWallet("wallet_2", 0, "USD")
	store.SeedWallet("wallet_3", 50_000, "USD")
}
