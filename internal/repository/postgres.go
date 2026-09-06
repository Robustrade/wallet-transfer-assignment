package repository

import (
	"database/sql"

	_ "github.com/lib/pq"
)

func NewPostgresDB(sourceName string) (*sql.DB, error) {
	db, err := sql.Open("postgres", sourceName)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		return nil, err
	}

	return db, nil
}
