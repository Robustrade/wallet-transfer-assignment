package database

import "database/sql"

func Healthy(db *sql.DB) error {
	return db.Ping()
}
