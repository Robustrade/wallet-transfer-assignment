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

	"github.com/amantiwari/wallet-transfer/internal/adapter/httpapi"
	"github.com/amantiwari/wallet-transfer/internal/adapter/postgres"
	"github.com/amantiwari/wallet-transfer/internal/app/transfer"
	"github.com/amantiwari/wallet-transfer/internal/app/wallet"
	"github.com/amantiwari/wallet-transfer/internal/platform/config"
	"github.com/amantiwari/wallet-transfer/internal/platform/logger"
	"github.com/amantiwari/wallet-transfer/internal/platform/system"
)

func main() {
	var err error
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		err = migrateOnly()
	} else {
		err = run()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "wallet-service: %s\n", err.Error())
		os.Exit(1)
	}
}

func migrateOnly() error {
	cfg, err := config.Load(os.Getenv("WALLET_CONFIG"))
	if err != nil {
		return err
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	log.Info("applying database migrations", "database", cfg.Database.RedactedTarget())
	return postgres.Migrate(context.Background(), cfg.Database.DSN())
}

func run() error {
	cfg, err := config.Load(os.Getenv("WALLET_CONFIG"))
	if err != nil {
		return err
	}
	log := logger.New(cfg.Log.Level, cfg.Log.Format)
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	db, err := postgres.Open(ctx, postgres.OpenConfig{
		DSN:                  cfg.Database.DSN(),
		MaxConns:             cfg.Database.MaxConns,
		MinConns:             cfg.Database.MinConns,
		MaxConnLifetime:      cfg.Database.MaxConnLifetime,
		MaxConnIdleTime:      cfg.Database.MaxConnIdleTime,
		ConnectRetries:       cfg.Database.ConnectRetries,
		ConnectRetryInterval: cfg.Database.ConnectRetryInterval,
		LockTimeout:          cfg.Database.LockTimeout,
		StatementTimeout:     cfg.Database.StatementTimeout,
	}, log)
	if err != nil {
		return err
	}
	defer db.Close()

	log.Info("applying database migrations", "database", cfg.Database.RedactedTarget())
	if err := postgres.Migrate(ctx, cfg.Database.DSN()); err != nil {
		return err
	}

	clock := system.Clock{}
	ids := system.IDs{}
	walletSvc := wallet.NewService(db, db.WalletReader(), clock, log)
	transferSvc := transfer.NewService(db, db.TransferReader(), db.LedgerReader(), clock, ids, log)
	router := httpapi.NewRouter(httpapi.Dependencies{
		Wallets:        walletSvc,
		Transfers:      transferSvc,
		DB:             db,
		Logger:         log,
		RequestTimeout: cfg.Server.RequestTimeout,
	})

	server := &http.Server{
		Addr:              cfg.Server.Address,
		Handler:           router,
		ReadHeaderTimeout: cfg.Server.ReadHeaderTimeout,
		ReadTimeout:       cfg.Server.ReadTimeout,
		WriteTimeout:      cfg.Server.WriteTimeout,
		IdleTimeout:       cfg.Server.IdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("server listening", "address", cfg.Server.Address)
		errCh <- server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Info("shutdown requested")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve http: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Server.ShutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown http server: %w", err)
	}
	log.Info("shutdown complete")
	return nil
}
