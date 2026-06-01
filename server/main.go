package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	domainledger "github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/ledger"
	domaintransfer "github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/transfer"
	domainwallet "github.com/yashbaheti1971/wallet-transfer-assignment/internal/domain/wallet"

	"github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres"
	pgledger "github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/ledger"
	pgtransfer "github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/transfer"
	pgwallet "github.com/yashbaheti1971/wallet-transfer-assignment/internal/platform/postgres/wallet"
)

func main() {
	// 1. Load configuration
	_ = godotenv.Load()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		dsn = "postgres://postgres:postgres@localhost:5432/walletdb?sslmode=disable"
	}

	// 2. Initialize Database & Run AutoMigrate
	db, err := postgres.NewConnection(dsn)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	gormDB := db.DB()

	// 3. Initialize Repositories
	walletRepo := pgwallet.New(gormDB)
	transferRepo := pgtransfer.New(gormDB)
	ledgerRepo := pgledger.NewLedger(gormDB)
	balanceRepo := pgledger.NewBalance(gormDB)

	// 4. Initialize Services
	walletSvc := domainwallet.NewService(walletRepo)
	ledgerSvc := domainledger.NewService(ledgerRepo, balanceRepo, db)
	// Transfer service requires wallet repository for existence checks and ledger service for double-entry bookkeeping
	transferSvc := domaintransfer.NewService(transferRepo, walletRepo, ledgerSvc)

	// 5. Initialize Handlers
	walletHandler := domainwallet.NewHandler(walletSvc)
	ledgerHandler := domainledger.NewHandler(ledgerSvc)
	transferHandler := domaintransfer.NewHandler(transferSvc)

	// 6. Setup Gin Router
	r := gin.Default()

	// Create API router group (e.g. at root or /api)
	apiGroup := r.Group("")

	// Register Routes for each domain
	walletHandler.RegisterRoutes(apiGroup)
	ledgerHandler.RegisterRoutes(apiGroup)
	transferHandler.RegisterRoutes(apiGroup)

	// 7. Start Server
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Server starting on port %s", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("Failed to run server: %v", err)
	}
}
