package platform

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

func NewDatabase() (*sql.DB, error) {
	db, err := sql.Open("sqlite", "file:wallet.db?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite database: %w", err)
	}

	return db, nil
}
