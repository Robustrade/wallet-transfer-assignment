// Package sqlite provides the SQLite dialect, connection setup and schema
// migrations, using the pure-Go modernc.org/sqlite driver (no CGO).
package sqlite

import (
	"context"
	"database/sql"
	"errors"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Dialect implements sqlrepo.Dialect for SQLite.
//
// Concurrency model: every write transaction starts with BEGIN IMMEDIATE
// (_txlock=immediate in the DSN), which takes the single database-wide write
// lock up front. Write transactions are therefore fully serialized, and
// there are no row locks to take. Waiters queue for up to busy_timeout
// (= LOCK_TIMEOUT).
type Dialect struct{}

func (Dialect) Name() string { return "sqlite" }

// Rebind is the identity: SQLite understands "?" natively.
func (Dialect) Rebind(query string) string { return query }

// LockClause is empty: SQLite has no SELECT ... FOR UPDATE, and it is not
// needed because BEGIN IMMEDIATE already holds the write lock.
func (Dialect) LockClause() string { return "" }

// TxOptions is nil. SQLite transactions are serializable by construction,
// and the IMMEDIATE behavior comes from the DSN.
func (Dialect) TxOptions() *sql.TxOptions { return nil }

// SetupTx does nothing: busy_timeout is set per connection in the DSN.
func (Dialect) SetupTx(context.Context, *sql.Tx) error { return nil }

// IsRetryable reports conflicts where waiting cannot help but restarting the
// transaction can:
//   - SQLITE_BUSY_SNAPSHOT: a WAL read transaction tried to write after
//     another connection committed. The busy handler is not invoked for
//     this; the transaction must restart.
//   - SQLITE_LOCKED (and its extended codes): a conflict inside the same
//     connection or shared cache.
func (Dialect) IsRetryable(err error) bool {
	code, ok := errorCode(err)
	if !ok {
		return false
	}
	return code == sqlite3.SQLITE_BUSY_SNAPSHOT || code&0xff == sqlite3.SQLITE_LOCKED
}

// IsLockTimeout reports SQLITE_BUSY other than BUSY_SNAPSHOT. The busy
// handler has already waited busy_timeout, so this is a lock timeout.
func (Dialect) IsLockTimeout(err error) bool {
	code, ok := errorCode(err)
	if !ok {
		return false
	}
	return code&0xff == sqlite3.SQLITE_BUSY && code != sqlite3.SQLITE_BUSY_SNAPSHOT
}

// IsUniqueViolation reports a PRIMARY KEY or UNIQUE constraint failure.
func (Dialect) IsUniqueViolation(err error) bool {
	code, ok := errorCode(err)
	if !ok {
		return false
	}
	return code == sqlite3.SQLITE_CONSTRAINT_UNIQUE || code == sqlite3.SQLITE_CONSTRAINT_PRIMARYKEY
}

// errorCode extracts the extended result code. The driver enables extended
// codes, so the low byte is the primary code (e.g. 5 = BUSY) and the high
// bits say which kind (e.g. 517 = BUSY_SNAPSHOT).
func errorCode(err error) (int, bool) {
	var se *sqlite.Error
	if !errors.As(err, &se) {
		return 0, false
	}
	return se.Code(), true
}
