// Package main is the entry point for the wallet transfer service.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/shubh/wallet-service/internal/config"
	"github.com/shubh/wallet-service/internal/db"
	"github.com/shubh/wallet-service/internal/handler"
	"github.com/shubh/wallet-service/internal/repository"
	"github.com/shubh/wallet-service/internal/service"
)

func main() {
	cfg := config.Load()

	// Connect to PostgreSQL
	database, err := db.Connect(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("database connection failed: %v", err)
	}
	defer database.Close()

	// Run migrations
	migrationPath := findMigrationPath()
	if err := db.RunMigrations(database, migrationPath); err != nil {
		log.Fatalf("migration failed: %v", err)
	}

	// Initialize repositories and TxManager
	txManager := db.NewSQLTxManager(database)
	walletRepo := repository.NewWalletRepository(database)
	transferRepo := repository.NewTransferRepository(database)
	ledgerRepo := repository.NewLedgerRepository(database)

	// Initialize service
	transferService := service.NewTransferService(txManager, walletRepo, transferRepo, ledgerRepo)

	// Initialize handlers
	transferHandler := handler.NewTransferHandler(transferService)
	walletHandler := handler.NewWalletHandler(transferService)
	healthHandler := handler.NewHealthHandler()

	// Set up router
	router := handler.NewRouter(transferHandler, walletHandler, healthHandler)

	// Start server with timeouts to prevent Slowloris and resource exhaustion
	addr := ":" + cfg.Port
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	log.Printf("server starting on %s", addr)
	if err := srv.ListenAndServe(); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}

// findMigrationPath locates the migration file relative to the executable.
func findMigrationPath() string {
	// Try relative to working directory first
	candidates := []string{
		"internal/db/migrations/001_init.sql",
		"migrations/001_init.sql",
	}

	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Try relative to executable
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		path := filepath.Join(dir, "migrations", "001_init.sql")
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}

	// Default fallback
	return "internal/db/migrations/001_init.sql"
}
