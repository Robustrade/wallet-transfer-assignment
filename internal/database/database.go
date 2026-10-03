package database

import (
	"fmt"
	"strings"
	"time"

	"wallet-transfer-assignment/internal/domain"

	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// InitDB initializes the GORM database connection using a single connection string.
func InitDB(databaseURL string) (*gorm.DB, error) {
	var dialector gorm.Dialector

	isSQLite := strings.HasPrefix(databaseURL, "sqlite:") || databaseURL == ":memory:"

	if isSQLite {
		sqlitePath := strings.TrimPrefix(databaseURL, "sqlite://")
		sqlitePath = strings.TrimPrefix(sqlitePath, "sqlite:")
		dialector = sqlite.Open(sqlitePath)
	} else {
		dialector = postgres.Open(databaseURL)
	}

	gormConfig := &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	}

	db, err := gorm.Open(dialector, gormConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	// Safe connection pool defaults
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(25)
	sqlDB.SetConnMaxLifetime(5 * time.Minute)

	// Run schema auto-migrations
	if err := AutoMigrate(db); err != nil {
		return nil, fmt.Errorf("failed to auto-migrate database tables: %w", err)
	}

	return db, nil
}

// AutoMigrate migrates all schema tables.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&domain.Wallet{},
		&domain.Transfer{},
		&domain.LedgerEntry{},
		&domain.IdempotencyRecord{},
	)
}
