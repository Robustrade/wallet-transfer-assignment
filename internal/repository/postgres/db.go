package postgres

import (
	"database/sql"
	"fmt"
	"wallet-transfer/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func EnsureDatabaseExists(host, port, user, password, dbName, sslMode string) error {
	adminConnStr := fmt.Sprintf("postgres://%s:%s@%s:%s/postgres?sslmode=%s", user, password, host, port, sslMode)
	db, err := sql.Open("pgx", adminConnStr)
	if err != nil {
		return fmt.Errorf("failed to open admin database connection: %w", err)
	}
	defer db.Close()

	var exists bool
	err = db.QueryRow("SELECT EXISTS(SELECT 1 FROM pg_database WHERE datname = $1)", dbName).Scan(&exists)
	if err != nil {
		return fmt.Errorf("failed to check if database exists: %w", err)
	}

	if !exists {
		// CREATE DATABASE cannot run inside a transaction or with parameter binding for name, so format is safe here.
		_, err = db.Exec(fmt.Sprintf("CREATE DATABASE %s", dbName))
		if err != nil {
			return fmt.Errorf("failed to create database %s: %w", dbName, err)
		}
	}
	return nil
}

func NewDB(connStr string) (*gorm.DB, error) {
	db, err := gorm.Open(postgres.Open(connStr), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql db: %w", err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	return db, nil
}

func RunMigrations(db *gorm.DB) error {
	err := db.AutoMigrate(
		&domain.Wallet{},
		&domain.Transfer{},
		&domain.LedgerEntry{},
		&domain.IdempotencyRecord{},
	)
	if err != nil {
		return fmt.Errorf("failed to run AutoMigrate: %w", err)
	}
	return nil
}
