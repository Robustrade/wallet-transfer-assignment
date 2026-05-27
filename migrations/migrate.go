package migrations

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
)

// RunMigrations executes the up migration
func RunMigrations(db *sql.DB) error {
	migrationFile := filepath.Join("migrations", "001_create_schema.up.sql")
	return executeMigration(db, migrationFile)
}

// RollbackMigrations executes the down migration
func RollbackMigrations(db *sql.DB) error {
	migrationFile := filepath.Join("migrations", "001_create_schema.down.sql")
	return executeMigration(db, migrationFile)
}

func executeMigration(db *sql.DB, filePath string) error {
	content, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read migration file %s: %w", filePath, err)
	}

	_, err = db.Exec(string(content))
	if err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", filePath, err)
	}

	fmt.Printf("Successfully executed migration: %s\n", filePath)
	return nil
}
