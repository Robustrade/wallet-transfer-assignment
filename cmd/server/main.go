package main

import (
	"database/sql"
	"log/slog"
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
	// Setup structured JSON logger for modern observability
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	// Load .env file if it exists (great for local development)
	_ = godotenv.Load()

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		slog.Error("DATABASE_URL environment variable is required")
		os.Exit(1)
	}

	// Connect to Database
	db, err := sql.Open("postgres", dbURL)
	if err != nil {
		slog.Error("Failed to open db connection", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := db.Ping(); err != nil {
		slog.Error("Failed to ping db", "error", err)
		os.Exit(1)
	}

	// Optimization: Production Database Connection Pooling
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)
	db.SetConnMaxLifetime(5 * time.Minute)

	slog.Info("Connected to PostgreSQL successfully", "conns", 25)

	// Initialize Layers
	repo := repository.NewPostgresRepository(db)

	// Transfer Module
	transferSvc := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferSvc)

	// Wallet Module
	walletSvc := service.NewWalletService(repo)
	walletAPIHandler := handler.NewWalletHandler(walletSvc)

	// Setup Router with Timeout Middleware Pattern
	mux := http.NewServeMux()

	transferEndpoint := http.HandlerFunc(transferHandler.CreateTransfer)
	transferTimeout := http.TimeoutHandler(transferEndpoint, 5*time.Second, `{"error": "request timeout"}`)
	mux.Handle("/transfers", transferTimeout)

	walletEndpoint := http.HandlerFunc(walletAPIHandler.GetWalletBalance)
	walletTimeout := http.TimeoutHandler(walletEndpoint, 3*time.Second, `{"error": "request timeout"}`)
	mux.Handle("/wallets/", walletTimeout) // Automatically delegates to history sub-routes

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	slog.Info("Starting server", "port", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}
