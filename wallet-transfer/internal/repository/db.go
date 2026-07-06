package repository

import (
	"database/sql"
	"log"

	_ "github.com/lib/pq"
)

// InitDB initializes the database connection pool
func InitDB(dsn string) (*sql.DB, error) {
	log.Println("Initializing database connection...")
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}

	// Configure connection pool
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(25)

	if err := db.Ping(); err != nil {
		return nil, err
	}

	log.Println("Database connection established")
	return db, nil
}
