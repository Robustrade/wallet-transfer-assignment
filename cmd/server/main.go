package main

import (
	"database/sql"
	"fmt"
	"log"
	"net/http"
	"time"
	"wallet-transfer-service/config"
	"wallet-transfer-service/internal/handlers"
	"wallet-transfer-service/internal/repositories"
	"wallet-transfer-service/internal/services"
	"wallet-transfer-service/migrations"

	_ "github.com/lib/pq"
)

func main() {
	// Load configuration
	cfg, err := config.Load("")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Connect to database
	db, err := sql.Open("postgres", cfg.Database.ConnectionString())
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Configure connection pool for production
	db.SetMaxOpenConns(25)                 // Maximum number of open connections
	db.SetMaxIdleConns(5)                  // Maximum number of idle connections
	db.SetConnMaxLifetime(5 * time.Minute) // Maximum lifetime of a connection

	// Test database connection
	if err := db.Ping(); err != nil {
		log.Fatalf("Failed to ping database: %v", err)
	}
	log.Println("Successfully connected to database")

	// Run migrations
	log.Println("Running database migrations...")
	if err := migrations.RunMigrations(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}
	log.Println("Database migrations completed")

	// Initialize repositories
	txManager := repositories.NewTransactionManager(db)
	walletRepo := repositories.NewWalletRepository(db)
	transferRepo := repositories.NewTransferRepository(db)
	ledgerRepo := repositories.NewLedgerRepository(db)
	idempotencyRepo := repositories.NewIdempotencyRepository(db)

	// Initialize services
	transferService := services.NewTransferService(
		txManager,
		walletRepo,
		transferRepo,
		ledgerRepo,
		idempotencyRepo,
	)

	// Initialize handlers
	transferHandler := handlers.NewTransferHandler(transferService)

	// Setup HTTP routes
	http.HandleFunc("/transfers", transferHandler.CreateTransfer)
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Start server
	addr := fmt.Sprintf(":%s", cfg.Server.Port)
	log.Printf("Starting server on %s", addr)
	if err := http.ListenAndServe(addr, nil); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}
