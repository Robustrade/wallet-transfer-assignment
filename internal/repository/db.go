package repository

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// OpenDatabase initializes a GORM PostgreSQL connection with connection pooling.
func OpenDatabase(databaseURL string, logLevel string) (*gorm.DB, error) {
	gormLogLevel := logger.Warn
	switch logLevel {
	case "debug":
		gormLogLevel = logger.Info
	case "info":
		gormLogLevel = logger.Warn
	case "warn":
		gormLogLevel = logger.Warn
	case "error":
		gormLogLevel = logger.Error
	}

	db, err := gorm.Open(postgres.Open(databaseURL), &gorm.Config{
		Logger: logger.Default.LogMode(gormLogLevel),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve underlying sql.DB: %w", err)
	}

	// Configure healthy connection pool settings
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(10 * time.Minute)
	sqlDB.SetConnMaxIdleTime(5 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	return db, nil
}

// AutoMigrate applies GORM AutoMigrate for domain models.
func AutoMigrate(db *gorm.DB) error {
	return db.AutoMigrate(
		&domain.User{},
		&domain.Wallet{},
		&domain.Transfer{},
		&domain.LedgerEntry{},
		&domain.IdempotencyRecord{},
	)
}

// ExecuteMigrationFile executes a raw SQL migration file against the database.
func ExecuteMigrationFile(db *gorm.DB, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read migration file %s: %w", filePath, err)
	}
	if err := db.Exec(string(content)).Error; err != nil {
		return fmt.Errorf("failed to execute migration file %s: %w", filePath, err)
	}
	log.Printf("Successfully applied migration: %s", filepath.Base(filePath))
	return nil
}
