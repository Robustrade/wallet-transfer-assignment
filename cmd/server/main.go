package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"wallet-transfer-assignment/internal/db"
	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

func main() {
	ctx := context.Background()
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://wallet_user:wallet_password@localhost:5433/wallet_transfer?sslmode=disable"
	}

	pool, err := db.NewPostgresPool(ctx, databaseURL)
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	defer pool.Close()

	walletRepository := repository.NewWalletRepository(pool)
	walletService := service.NewWalletService(walletRepository)

	transferRepository := repository.NewTransferRepository(pool)
	transferService := service.NewTransferService(transferRepository)

	if err := http.ListenAndServe(":8080", handler.NewRouter(walletService, transferService)); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
