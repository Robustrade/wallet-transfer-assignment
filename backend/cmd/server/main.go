package main

import (
	"log"
	"net/http"
	"os"
	"wallet-transfer/database"
	handler "wallet-transfer/handlers"
	"wallet-transfer/repository"
	"wallet-transfer/service"
)

func main() {

	databaseURL := os.Getenv("DATABASE_URL")

	if databaseURL == "" {
		log.Fatal("DATABASE_URL is required")
	}

	db, err := database.NewPostgresPool(databaseURL)
	if err != nil {
		log.Fatal(err)
	}

	defer db.Close()

	walletRepo :=
		repository.NewWalletRepository(db)

	transferRepo :=
		repository.NewTransferRepository(db)

	ledgerRepo :=
		repository.NewLedgerRepository(db)


	transferService :=
		service.NewTransferService(
			db,
			walletRepo,
			transferRepo,
			ledgerRepo,
		)


	transferHandler :=
		handler.NewTransferHandler(
			transferService,
		)

	mux := http.NewServeMux()

	mux.HandleFunc(
		"POST /transfers",
		transferHandler.CreateTransfer,
	)

	log.Println("server started on :8080")

	if err := http.ListenAndServe(
		":8080",
		mux,
	); err != nil {

		log.Fatal(err)
	}
}
