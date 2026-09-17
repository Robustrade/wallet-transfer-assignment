// Package db provides database connection and migration utilities.
package db

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"time"

	_ "github.com/lib/pq"
)

// Connect establishes a connection pool to PostgreSQL and verifies connectivity.
func Connect(databaseURL string) (*sql.DB, error) {
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Connection pool settings
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(10)
	db.SetConnMaxLifetime(5 * time.Minute)

	// Verify connectivity with retries
	for i := 0; i < 10; i++ {
		err = db.Ping()
		if err == nil {
			log.Println("database connection established")
			return db, nil
		}
		log.Printf("waiting for database... attempt %d/10: %v", i+1, err)
		time.Sleep(2 * time.Second)
	}

	return nil, fmt.Errorf("failed to connect to database after retries: %w", err)
}

// RunMigrations executes the SQL migration file against the database.
func RunMigrations(db *sql.DB, migrationPath string) error {
	content, err := os.ReadFile(migrationPath)
	if err != nil {
		return fmt.Errorf("failed to read migration file: %w", err)
	}

	_, err = db.Exec(string(content))
	if err != nil {
		return fmt.Errorf("failed to execute migration: %w", err)
	}

	log.Printf("migration applied: %s", migrationPath)
	return nil
}
