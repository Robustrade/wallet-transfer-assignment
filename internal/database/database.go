package database

import (
	"database/sql"
	"fmt"

	"github.com/wallet-transfer-assignment/internal/config"
	"github.com/wallet-transfer-assignment/migrations"
)

// NewDB creates a database connection based on the configured driver.
func NewDB(cfg *config.Config) (*sql.DB, error) {
	switch cfg.DBDriver {
	case "sqlite":
		return NewSQLiteDB(cfg.DatabaseURL)
	case "postgres":
		return NewPostgresDB(cfg.DatabaseURL)
	default:
		return nil, fmt.Errorf("unsupported database driver: %s", cfg.DBDriver)
	}
}

// RunMigrations runs SQL scripts from migrations/{driver}/ in lexical order.
func RunMigrations(db *sql.DB, driver string) error {
	if err := migrations.Apply(db, driver); err != nil {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}

// RunSQLiteMigrations runs SQLite migrations (convenience for tests).
func RunSQLiteMigrations(db *sql.DB) error {
	return RunMigrations(db, "sqlite")
}

// RunPostgresMigrations runs PostgreSQL migrations (convenience for tests).
func RunPostgresMigrations(db *sql.DB) error {
	return RunMigrations(db, "postgres")
}
