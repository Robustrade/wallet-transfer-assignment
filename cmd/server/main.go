package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/mshabab1296/wallet-transfer-assignment/internal/httpapi"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/repository"
	"github.com/mshabab1296/wallet-transfer-assignment/internal/service"
)

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://wallet:wallet@localhost:5433/wallets?sslmode=disable"
	}
	repo, err := repository.OpenPostgres(context.Background(), dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer repo.Close()
	handler := httpapi.New(service.NewTransferService(repo))
	log.Printf("wallet transfer service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", handler.Routes()))
}
