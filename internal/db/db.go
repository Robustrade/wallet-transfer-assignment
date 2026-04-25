package db

import (
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/robustrade/wallet-transfer-assignment/config"
)

func NewPostgresDB(cfg config.DatabaseConfig) (*sql.DB, error) {
	db, err := sql.Open(cfg.Driver, cfg.ConnectionString())
	if err != nil {
		return nil, fmt.Errorf("failed to open DB: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(time.Duration(cfg.ConnMaxLifetime) * time.Minute)

	// Verify connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping DB: %w", err)
	}

	return db, nil
}
