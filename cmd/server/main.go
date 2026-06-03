package main

import (
	"log"
	"wallet-transfer/internal/config"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository/postgres"
	"wallet-transfer/internal/router"
	"wallet-transfer/internal/service"
)

func main() {
	log.Println("Starting Wallet Transfer Service (Gin & GORM)...")

	// 1. Load config
	cfg := config.Load()

	// 2. Ensure database exists
	log.Printf("Ensuring database %s exists...", cfg.DBName)
	if err := postgres.EnsureDatabaseExists(cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSSLMode); err != nil {
		log.Fatalf("Failed to ensure database exists: %v", err)
	}

	// 3. Initialize DB with GORM
	log.Printf("Connecting to database at %s:%s/%s...", cfg.DBHost, cfg.DBPort, cfg.DBName)
	db, err := postgres.NewDB(cfg.DBConnStr())
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}

	// 4. Run GORM migrations
	log.Println("Running AutoMigrate...")
	if err := postgres.RunMigrations(db); err != nil {
		log.Fatalf("Migration run failed: %v", err)
	}
	log.Println("AutoMigrate completed successfully.")

	// 5. Initialize repositories
	walletRepo := postgres.NewWalletRepository()
	transferRepo := postgres.NewTransferRepository()
	ledgerRepo := postgres.NewLedgerRepository()
	idempotencyRepo := postgres.NewIdempotencyRepository()

	// 6. Initialize service
	transferService := service.NewTransferService(
		db,
		walletRepo,
		transferRepo,
		ledgerRepo,
		idempotencyRepo,
	)

	// 7. Initialize handler
	h := handler.NewTransferHandler(transferService, walletRepo, db)

	// 8. Setup Router
	r := router.Setup(h)

	// 9. Start server
	log.Printf("Server starting on port %s...", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
