package main

import (
	"log"
	"net/http"
	"os"

	"wallet-transfer-assignment/internal/handler"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
	"wallet-transfer-assignment/internal/test_util"
)

func main() {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("DATABASE_URL is not set")
	}

	db, err := repository.NewPostgresDB(databaseURL)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	if err := testutil.RunMigrations(databaseURL, "migrations"); err != nil {
		log.Fatal(err)
	}

	walletRepo := repository.NewWalletRepository()
	transferRepo := repository.NewTransferRepository()
	ledgerRepo := repository.NewLedgerRepository()
	idempotencyRepo := repository.NewIdempotencyRepository()

	transferService := service.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	transferHandler := handler.NewTransferHandler(transferService)

	router := handler.NewRouter(transferHandler)

	log.Println("server listening on :8080")

	if err := http.ListenAndServe(":8080", router); err != nil {
		log.Fatal(err)
	}
}
