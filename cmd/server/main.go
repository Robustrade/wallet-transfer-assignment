package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/db"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/handler"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/repository"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/service"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/wallet_transfer?sslmode=disable"
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = ":8080"
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	pool, err := db.NewPool(ctx, dsn)
	if err != nil {
		log.Fatalf("connecting to database: %v", err)
	}
	defer pool.Close()

	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()

	transferService := service.NewTransferService(pool, walletRepo, transferRepo, ledgerRepo)
	walletService := service.NewWalletService(pool, walletRepo)

	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(walletService)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transferHandler.Create)
	mux.HandleFunc("GET /transfers/{id}", func(w http.ResponseWriter, r *http.Request) {
		transferHandler.Get(w, r, r.PathValue("id"))
	})
	mux.HandleFunc("POST /wallets", walletHandler.Create)
	mux.HandleFunc("GET /wallets/{id}", func(w http.ResponseWriter, r *http.Request) {
		walletHandler.Get(w, r, r.PathValue("id"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		log.Printf("wallet-transfer-service listening on %s", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server error: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	<-stop

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}
}
