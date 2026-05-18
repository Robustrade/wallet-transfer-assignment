package database

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite" // Pure-Go SQLite driver
)

// NewSQLiteDB opens a SQLite database with WAL mode, foreign keys, and busy timeout.
func NewSQLiteDB(dataSourceName string) (*sql.DB, error) {
	dsn := dataSourceName
	if dataSourceName == ":memory:" {
		// For in-memory, use a shared cache so that all connections see the same data
		dsn = "file::memory:?cache=shared"
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// SQLite performs best with a single connection for writes
	db.SetMaxOpenConns(1)

	// Set pragmas after opening the connection
	pragmas := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
	}
	for _, pragma := range pragmas {
		if _, err := db.Exec(pragma); err != nil {
			return nil, fmt.Errorf("failed to set pragma %q: %w", pragma, err)
		}
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping sqlite database: %w", err)
	}

	return db, nil
}
