package main

import (
	"database/sql"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/joho/godotenv"
	_ "github.com/lib/pq"
)

func main() {
	// Load .env file if it exists (great for local development)
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		log.Fatal("DATABASE_URL environment variable is required")
	}

	// Connect to Database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		log.Fatalf("Failed to open db connection: %v", err)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping db: %v", err)
	}

	// Optimization: Production Database Connection Pooling
	// tuning these allows high throughput without exhausting Postgres RAM.
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	log.Println("Connected to PostgreSQL successfully")

	// Initialize Layers
	repo := repository.NewPostgresRepository(db)

	// Transfer Module
	transferSvc := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferSvc)

	// Wallet Module (Optional Enhancement)
	walletSvc := service.NewWalletService(repo)
	walletAPIHandler := handler.NewWalletHandler(walletSvc)

	// Setup Router with Timeout Middleware Pattern (Golang Best Practice)
	mux := http.NewServeMux()

	// Transfer Routes
	transferEndpoint := http.HandlerFunc(transferHandler.CreateTransfer)
	transferTimeout := http.TimeoutHandler(transferEndpoint, 5*time.Second, `{"error": "request timeout"}`)
	mux.Handle("/transfers", transferTimeout)

	// Wallet Routes
	walletEndpoint := http.HandlerFunc(walletAPIHandler.GetWalletBalance)
	walletTimeout := http.TimeoutHandler(walletEndpoint, 3*time.Second, `{"error": "request timeout"}`)
	mux.Handle("/wallets/", walletTimeout)

	// A simple health check endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on port %s", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
