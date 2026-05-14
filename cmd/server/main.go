package main

import (
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/manish8561/wallet-transfer-assignment/cmd/config"
	"github.com/manish8561/wallet-transfer-assignment/internal/handlers"
	"github.com/manish8561/wallet-transfer-assignment/internal/repositories"
	"github.com/manish8561/wallet-transfer-assignment/internal/services"
)

func main() {
	log.Println("Starting the server...")

	// Database configuration
	db := config.ConnectDB()

	// Run migrations
	config.RunMigrations(db)

	// Initialize repositories
	walletRepo := repositories.NewWalletRepository(db)
	transferRepo := repositories.NewTransferRepository(db)
	ledgerRepo := repositories.NewLedgerRepository(db)
	idempotencyRepo := repositories.NewIdempotencyRepository(db)

	// Initialize service
	transferService := services.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo)

	// Initialize handlers
	transferHandler := handlers.NewTransferHandler(transferService)

	// Setup router
	router := http.NewServeMux()

	// Register routes
	router.HandleFunc("POST /transfers", transferHandler.CreateTransfer)
	router.HandleFunc("POST /wallets", transferHandler.CreateWallet)
	router.HandleFunc("GET /wallets/balance/{wallet_id}", transferHandler.GetBalance)

	// Health check route
	router.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "OK")
	})

	// Start server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
