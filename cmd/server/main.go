package main

import (
	"context"
	"log"
	"log/slog"
	"net/http"
	"os"

	"wallet-transfer/internal/db"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	ctx := context.Background()

	// ── Database ──────────────────────────────────────────────────────────
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable"
	}

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}
	defer pool.Close()

	slog.Info("database connected")

	// ── Dependency wiring ─────────────────────────────────────────────────
	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()

	svc := service.New(pool, walletRepo, transferRepo, ledgerRepo)
	h := handler.New(svc)

	// ── Router ────────────────────────────────────────────────────────────
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Post("/wallets", h.CreateWallet)
	r.Get("/wallets/{walletId}", h.GetWallet)
	r.Get("/wallets/{walletId}/transfers", h.GetTransferHistory)
	r.Post("/transfers", h.CreateTransfer)

	// ── Server ────────────────────────────────────────────────────────────
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("server starting", "port", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
