package main

import (
	"database/sql"
	"fmt"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func connectDB() (*sql.DB, error) {
	dsn := "postgres://pratiskha@localhost:5432/wallet_transfer?sslmode=disable"

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("database connection failed: %w", err)
	}

	return db, nil
}
