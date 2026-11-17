package main

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/skc1234/wallet-transfer-assignment/internal/config"
	"github.com/skc1234/wallet-transfer-assignment/internal/handler"
	"github.com/skc1234/wallet-transfer-assignment/internal/repository"
	"github.com/skc1234/wallet-transfer-assignment/internal/service"
)

type HealthResponse struct {
	Status  string `json:"status"`
	Message string `json:"message"`
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	json.NewEncoder(w).Encode(HealthResponse{
		Status:  "success",
		Message: "Wallet Transfer API is running",
	})
}

func main() {
	// Database connection
	db, err := config.ConnectDB()
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}
	defer db.Close()

	log.Println("PostgreSQL connected successfully!")

	// Wallet dependencies
	walletRepo := repository.NewWalletRepository(db)
	walletHandler := handler.NewWalletHandler(walletRepo)

	// Transfer dependencies
	transferRepo := repository.NewTransferRepository(db)
	transferService := service.NewTransferService(transferRepo)
	transferHandler := handler.NewTransferHandler(transferService)

	// Router
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", healthHandler)

	mux.HandleFunc("POST /wallets", walletHandler.CreateWallet)
	mux.HandleFunc("GET /wallets/{id}", walletHandler.GetWallet)

	mux.HandleFunc("POST /transfers", transferHandler.CreateTransfer)

	// Start server
	log.Println("Wallet Transfer API running on port 8081")

	if err := http.ListenAndServe(":8081", mux); err != nil {
		log.Fatal(err)
	}
}
