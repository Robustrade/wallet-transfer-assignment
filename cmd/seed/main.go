package main

import (
	"log"

	"wallet-transfer-assignment/internal/models"
	"wallet-transfer-assignment/internal/services/database"
)

func main() {
	// 1. Manually drop existing columns before InitDB because GORM's AutoMigrate
	// will fail to cast timestamp with time zone to bigint.
	// We use the DB from the config directly here or just run InitDB and handle it before models.
	database.InitDB()

	database.DB.Exec("ALTER TABLE wallets DROP COLUMN IF EXISTS created_at CASCADE")
	database.DB.Exec("ALTER TABLE wallets DROP COLUMN IF EXISTS updated_at CASCADE")
	database.DB.Exec("ALTER TABLE transfers DROP COLUMN IF EXISTS created_at CASCADE")
	database.DB.Exec("ALTER TABLE transfers DROP COLUMN IF EXISTS updated_at CASCADE")
	database.DB.Exec("ALTER TABLE ledgers DROP COLUMN IF EXISTS created_at CASCADE")

	// 2. Re-run migration now that conflicting columns are gone
	if err := database.DB.AutoMigrate(&models.Wallet{}, &models.Transfer{}, &models.Ledger{}); err != nil {
		log.Fatalf("failed to migrate database: %v", err)
	}

	// 3. Truncate tables for a clean seed
	database.DB.Exec("TRUNCATE TABLE ledgers, transfers, wallets CASCADE")

	wallets := []models.Wallet{
		{ID: "1a22ae5d-438b-419c-aafd-5e999ec7f2e4", Balance: 100000}, // 1000.00
		{ID: "50793c1f-4af0-4cd8-af13-180b8f0d5fe3", Balance: 50000},  // 500.00
		{ID: "953d4907-13e4-45cd-9e3b-8148372fb3c1", Balance: 0},
	}

	for _, w := range wallets {
		if err := database.DB.Create(&w).Error; err != nil {
			log.Printf("failed to create wallet %s: %v", w.ID, err)
		} else {
			log.Printf("Created wallet %s with balance %d", w.ID, w.Balance)
		}
	}
}
