package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"wallet-transfer/internal/handler"
	postgresrepo "wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/service"
)

func main() {
	ctx := context.Background()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		databaseURL = "postgres://wallet:wallet@localhost:5432/walletdb?sslmode=disable"
	}

	db, err := postgresrepo.NewDB(ctx, databaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer db.Close()

	repo := postgresrepo.NewRepository(db)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	mux := http.NewServeMux()

	mux.HandleFunc("/transfers", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		transferHandler.CreateTransfer(w, r)
	})

	log.Println("server running on http://localhost:8080")

	if err := http.ListenAndServe(":8080", mux); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
