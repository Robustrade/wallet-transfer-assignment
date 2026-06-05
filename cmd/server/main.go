package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/db"
	"wallet-transfer-service/internal/handler"
	"wallet-transfer-service/internal/repository"
	"wallet-transfer-service/internal/service"
)

func main() {

	connStr := os.Getenv("DATABASE_URL")

	if connStr == "" {
		connStr = "postgres://postgres:postgres@localhost:5432/walletdb?sslmode=disable"
	}

	pool, err :=
		db.NewPool(
			connStr,
		)

	if err != nil {
		log.Fatal(err)
	}

	walletRepo :=
		repository.NewWalletRepository()

	transferRepo :=
		repository.NewTransferRepository()

	ledgerRepo :=
		repository.NewLedgerRepository()

	transferService :=
		service.NewTransferService(
			pool,
			walletRepo,
			transferRepo,
			ledgerRepo,
		)

	transferHandler :=
		handler.NewTransferHandler(
			transferService,
		)
	router := gin.Default()

	router.POST(
		"/transfers",
		transferHandler.CreateTransfer,
	)

	log.Fatal(
		router.Run(":8080"),
	)
}
