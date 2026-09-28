package postgres

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations holds the embedded schema files, compiled into the binary.
var Migrations, _ = fs.Sub(migrationFiles, "migrations")

// maxConns caps the pool. Excess requests then wait for a connection (bounded
// by the request context) instead of exhausting the server's
// max_connections.
const maxConns = 20

// Open returns a connection pool. sql.Open does not connect; the caller
// should Ping.
func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	db.SetMaxOpenConns(maxConns)
	db.SetMaxIdleConns(maxConns) // keep connections warm under bursts
	return db, nil
}
