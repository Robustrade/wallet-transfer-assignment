// Package postgres holds the GORM-backed implementations of the repository
// interfaces declared in the sibling iface package.
//
// We use GORM strictly as a connection / transaction manager and as a
// raw-SQL executor (`tx.Raw(...).Row().Scan(...)`, `tx.Exec(...)`). We do
// NOT use GORM's ORM, query-builder, AutoMigrate, hooks, or struct-scan
// features — every statement issued from this package is hand-written
// parameterized SQL, so the database schema is owned by the SQL files
// under `migrations/` rather than by reflection over Go structs.
package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// txKey is the private context key under which an active transactional
// *gorm.DB is stored.
type txKey struct{}

// withTx returns a new context carrying the given transactional *gorm.DB.
// A nil tx is rejected: storing it would let mustTxQuerier hand back a nil
// pointer that panics on first use, which is the opposite of what the
// missing-tx error path is for.
func withTx(ctx context.Context, tx *gorm.DB) context.Context {
	if tx == nil {
		return ctx
	}
	return context.WithValue(ctx, txKey{}, tx)
}

// txFromContext returns the active transactional *gorm.DB from the context,
// if any. Returns ok=false if no value is stored OR if the stored value is
// a typed-nil *gorm.DB.
func txFromContext(ctx context.Context) (*gorm.DB, bool) {
	tx, ok := ctx.Value(txKey{}).(*gorm.DB)
	if !ok || tx == nil {
		return nil, false
	}
	return tx, true
}

// errMissingTx is returned by mustTxQuerier when no transaction is in the
// context. Repository write paths require a transaction so that a
// SELECT FOR UPDATE actually holds row locks past the end of the statement;
// falling back to the raw pool would silently break the concurrency contract.
var errMissingTx = errors.New("repository: operation requires an active transaction in the context")

// mustTxQuerier returns the active transactional *gorm.DB from the context,
// or errMissingTx if no transaction is wired. Use this in any repository
// method that must run inside the same transaction as its surrounding work
// (i.e., everything that writes to the DB or holds row locks).
func mustTxQuerier(ctx context.Context) (*gorm.DB, error) {
	tx, ok := txFromContext(ctx)
	if !ok {
		return nil, errMissingTx
	}
	return tx.WithContext(ctx), nil
}

// Postgres SQLSTATE codes used by repository error mapping.
const (
	sqlstateForeignKeyViolation = "23503"
	sqlstateCheckViolation      = "23514"
)

// Constraint names emitted by Postgres for the schema in `migrations/`.
// These follow Postgres's default naming for inline column constraints
// (`<table>_<column>_<suffix>`). Mapping a SQLSTATE-only check to a
// specific business error is fragile — a future schema change could add
// a new constraint that fires the same SQLSTATE — so error mapping always
// pairs the SQLSTATE with the expected constraint name.
const (
	constraintWalletsBalanceCheck     = "wallets_balance_minor_check"
	constraintTransfersFromWalletFKey = "transfers_from_wallet_id_fkey"
	constraintTransfersToWalletFKey   = "transfers_to_wallet_id_fkey"
)

// isPgConstraintViolation reports whether err is a Postgres error with the
// given SQLSTATE *and* the given constraint name. Pairing SQLSTATE with the
// constraint name keeps error mapping precise: a generic 23514 mapping to
// ErrInsufficientFunds would misclassify any new CHECK that gets added later
// to the same table.
//
// GORM passes the underlying pgx driver error through verbatim (we set
// TranslateError=false in the gorm config), so errors.As reaches the
// *pgconn.PgError without translation.
func isPgConstraintViolation(err error, sqlstate, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.SQLState() == sqlstate && pgErr.ConstraintName == constraint
}
