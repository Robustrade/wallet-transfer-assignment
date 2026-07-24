package db

import (
	"log"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"wallet-assignment/internal/domain"
)

func Init() *gorm.DB {
	db, err := gorm.Open(sqlite.Open("/app/data/wallet.db"), &gorm.Config{})
	if err != nil {
		panic(err)
	}

	if err := db.Exec("PRAGMA foreign_keys = ON").Error; err != nil {
		panic(err)
	}

	if err := db.AutoMigrate(
		&domain.Wallet{},
		&domain.Transfer{},
		&domain.LedgerEntry{},
		&domain.IdempotencyRecord{},
	); err != nil {
		panic(err)
	}

	log.Println("DB initialized")
	return db
}
