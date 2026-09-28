// Package postgres provides the PostgreSQL dialect, connection setup and
// schema migrations, using pgx through its database/sql adapter.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

// SQLSTATE codes we act on.
// https://www.postgresql.org/docs/current/errcodes-appendix.html
const (
	codeSerializationFailure = "40001"
	codeDeadlockDetected     = "40P01"
	codeLockNotAvailable     = "55P03" // raised when lock_timeout expires
	codeUniqueViolation      = "23505"
)

// Dialect implements sqlrepo.Dialect for PostgreSQL.
//
// Concurrency model: READ COMMITTED plus explicit row locks. The transfer
// locks both wallet rows with SELECT ... FOR UPDATE, always in sorted id
// order. A concurrent transfer touching either wallet waits, up to
// lock_timeout, and then reads the committed balance.
type Dialect struct {
	lockTimeout time.Duration
}

// NewDialect returns a dialect that applies lockTimeout to every transaction.
func NewDialect(lockTimeout time.Duration) Dialect {
	return Dialect{lockTimeout: lockTimeout}
}

func (Dialect) Name() string { return "postgres" }

// Rebind rewrites "?" placeholders as "$1", "$2", ... in order.
func (Dialect) Rebind(query string) string {
	var b strings.Builder
	b.Grow(len(query) + 8)
	n := 0
	for _, r := range query {
		if r == '?' {
			n++
			b.WriteByte('$')
			b.WriteString(strconv.Itoa(n))
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func (Dialect) LockClause() string { return " FOR UPDATE" }

// TxOptions uses READ COMMITTED. The rows the transfer decides on are locked
// explicitly, so a stricter level would only add serialization failures
// without adding safety.
func (Dialect) TxOptions() *sql.TxOptions {
	return &sql.TxOptions{Isolation: sql.LevelReadCommitted}
}

// SetupTx sets lock_timeout for this transaction only. The final "true" of
// set_config is the SET LOCAL behavior: the setting ends with the transaction
// and does not leak to the next user of the pooled connection. set_config is
// used instead of SET because SET does not accept a bind parameter.
func (d Dialect) SetupTx(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT set_config('lock_timeout', $1, true)`,
		fmt.Sprintf("%dms", d.lockTimeout.Milliseconds()))
	return err
}

// IsRetryable reports deadlocks and serialization failures. Postgres has
// already aborted the transaction, so it must be restarted from BEGIN.
func (Dialect) IsRetryable(err error) bool {
	code := sqlState(err)
	return code == codeDeadlockDetected || code == codeSerializationFailure
}

func (Dialect) IsLockTimeout(err error) bool { return sqlState(err) == codeLockNotAvailable }

func (Dialect) IsUniqueViolation(err error) bool { return sqlState(err) == codeUniqueViolation }

// sqlState returns the SQLSTATE of a Postgres error anywhere in err's chain,
// or "" if there is none.
func sqlState(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
