package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" database/sql driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrations holds the embedded schema files, compiled into the binary.
var Migrations, _ = fs.Sub(migrationFiles, "migrations")

// Open returns a pool whose every connection is configured for safe
// concurrent use. The settings are DSN parameters, so the driver applies them
// to each new connection (a PRAGMA run once would affect only one
// connection):
//
//   - busy_timeout(LOCK_TIMEOUT): wait for the write lock instead of failing
//     immediately. Set first, because switching to WAL may itself need the
//     lock.
//   - foreign_keys(1): SQLite ignores REFERENCES clauses unless this is on.
//   - journal_mode(WAL): readers do not block the writer, and the writer
//     does not block readers.
//   - _txlock=immediate: BeginTx issues BEGIN IMMEDIATE. The write lock is
//     taken at the start, so two transactions cannot both read a balance and
//     then race to write it.
//
// sql.Open does not connect; the caller should Ping.
func Open(dsn string, lockTimeout time.Duration) (*sql.DB, error) {
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	full := dsn + sep + fmt.Sprintf(
		"_pragma=busy_timeout(%d)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_txlock=immediate",
		lockTimeout.Milliseconds())

	db, err := sql.Open("sqlite", full)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(readPoolSize)
	db.SetMaxIdleConns(readPoolSize)
	return db, nil
}

// OpenWriter returns a pool with exactly ONE connection, used for all write
// transactions.
//
// SQLite allows only one writer at a time. If several pooled connections
// compete for the write lock, they busy-poll, and SQLite's busy handler is
// not a fair queue: under load, an unlucky connection can keep losing until
// busy_timeout expires. The 500-transfer stress test showed exactly this,
// first with an unbounded pool, and less often with a pool of 4.
//
// With a single writer connection, write transactions queue in database/sql
// instead, bounded by each request's context (REQUEST_TIMEOUT), and never
// fight each other inside SQLite. busy_timeout still covers contention from
// outside this pool, such as another process or ad-hoc SQL.
func OpenWriter(dsn string, lockTimeout time.Duration) (*sql.DB, error) {
	db, err := Open(dsn, lockTimeout)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}

// readPoolSize is the connection count for reads (and for migrations and
// pings). In WAL mode, readers run concurrently with the single writer.
const readPoolSize = 4
