package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://wallet:wallet@127.0.0.1:5433/wallet_transfer"
	}

	pool, err := database.NewPool(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer pool.Close()

	walletRepo := repository.NewPostgresWalletRepository()
	transferRepo := repository.NewPostgresTransferRepository()
	ledgerRepo := repository.NewPostgresLedgerRepository()

	transferService := service.NewTransferService(
		pool,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	transferHandler := handler.NewTransferHandler(
		transferService,
	)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	http.HandleFunc(
		"/transfers",
		transferHandler.CreateTransfer,
	)
	log.Println("server listening on :8080")

	if err := http.ListenAndServe(":8080", nil); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
