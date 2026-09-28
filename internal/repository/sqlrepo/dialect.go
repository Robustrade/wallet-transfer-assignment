// Package sqlrepo implements the service ports with database/sql. The SQL is
// written once, with "?" placeholders. Everything that differs between SQLite
// and PostgreSQL goes through the Dialect interface.
package sqlrepo

import (
	"context"
	"database/sql"
)

// Dialect captures everything that differs between the supported databases.
type Dialect interface {
	// Name is "sqlite" or "postgres".
	Name() string

	// Rebind converts "?" placeholders to the driver's style ("$1", "$2", ...
	// for Postgres). Queries must not contain a literal '?' character.
	Rebind(query string) string

	// LockClause is appended to SELECTs that must lock the rows they read:
	// " FOR UPDATE" on Postgres, and "" on SQLite, where BEGIN IMMEDIATE
	// already holds the whole-database write lock.
	LockClause() string

	// TxOptions are passed to BeginTx (READ COMMITTED on Postgres).
	TxOptions() *sql.TxOptions

	// SetupTx runs once at the start of every transaction (Postgres sets
	// lock_timeout for that transaction only).
	SetupTx(ctx context.Context, tx *sql.Tx) error

	// IsRetryable reports a conflict that restarting the whole transaction
	// can fix: deadlock or serialization failure on Postgres,
	// SQLITE_BUSY_SNAPSHOT / SQLITE_LOCKED on SQLite.
	IsRetryable(err error) bool

	// IsLockTimeout reports that the transaction gave up waiting for a lock
	// after LOCK_TIMEOUT. It is NOT retried: the wait already used up most of
	// the request's time budget.
	IsLockTimeout(err error) bool

	// IsUniqueViolation reports a primary key or UNIQUE constraint failure.
	IsUniqueViolation(err error) bool
}
