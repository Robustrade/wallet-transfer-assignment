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

	"github.com/go-chi/chi/v5"
	chimw "github.com/go-chi/chi/v5/middleware"
	"github.com/wallet-transfer-assignment/internal/handler"
	"github.com/wallet-transfer-assignment/internal/middleware"
	"github.com/wallet-transfer-assignment/internal/repository"
	"github.com/wallet-transfer-assignment/internal/service"
)

func main() {
	dsn := mustEnv("DATABASE_URL")
	addr := envOr("SERVER_ADDR", ":8080")

	ctx := context.Background()

	pool, err := repository.NewPool(ctx, dsn)
	if err != nil {
		slog.Error("cannot connect to database", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Wire up repositories, service, and handlers.
	walletRepo := repository.NewWalletRepository(pool)
	transferRepo := repository.NewTransferRepository(pool)
	ledgerRepo := repository.NewLedgerEntryRepository(pool)

	transferSvc := service.NewTransferService(pool, walletRepo, transferRepo, ledgerRepo)

	transferH := handler.NewTransferHandler(transferSvc)
	walletH := handler.NewWalletHandler(walletRepo)

	r := chi.NewRouter()
	r.Use(chimw.Recoverer)
	r.Use(middleware.RequestLogger)

	r.Post("/transfers", transferH.CreateTransfer)
	r.Get("/wallets/{walletId}", walletH.GetWallet)

	// Health-check for load balancers / readiness probes.
	r.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:         addr,
		Handler:      r,
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	// Graceful shutdown on SIGINT / SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	}()

	<-quit
	slog.Info("shutting down")

	shutCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		slog.Error("graceful shutdown failed", "err", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		slog.Error("required environment variable not set", "key", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
