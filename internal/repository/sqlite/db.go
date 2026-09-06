// Package sqlite implements the repository interfaces on top of SQLite via
// the pure-Go modernc.org/sqlite driver (no cgo toolchain required).
package sqlite

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

//go:embed migrations/0001_init.sql
var initSchema string

// Open opens a SQLite database at path (use "file::memory:?cache=shared" for
// an in-process test database) and applies the schema.
//
// _txlock=immediate makes every BeginTx acquire SQLite's write lock up front
// (BEGIN IMMEDIATE) instead of upgrading lazily from a read lock, so
// concurrent writers fail fast/serialize instead of deadlocking against each
// other under SQLITE_BUSY. Combined with a single open connection, this
// gives transfer transactions the same "one writer at a time" guarantee
// Postgres would give via SELECT ... FOR UPDATE row locks.
func Open(ctx context.Context, path string) (*sql.DB, error) {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	dsn := path + sep + "_txlock=immediate&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}

	// SQLite has no meaningful concurrent-writer story; a single connection
	// avoids SQLITE_BUSY races between Go-level pooled connections and makes
	// _txlock=immediate serialize all writers deterministically.
	db.SetMaxOpenConns(1)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}

	if _, err := db.ExecContext(ctx, initSchema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	return db, nil
}
