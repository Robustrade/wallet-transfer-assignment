// Package repository holds every SQL statement in the service and translates
// driver errors into the domain vocabulary. It performs persistence only: it
// decides nothing about what a transfer should do, only how a row is read or
// written.
package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

// PostgreSQL error codes this layer reacts to. Everything else is passed up as
// an unexpected error.
const (
	sqlstateForeignKeyViolation = "23503"
	sqlstateLockNotAvailable    = "55P03"
)

// Constraint names from the migration. The handler's 404 depends on telling
// these apart, which is why they are named explicitly in the schema rather than
// left to PostgreSQL's defaults.
const (
	constraintFromWalletFK = "transfers_from_wallet_fk"
	constraintToWalletFK   = "transfers_to_wallet_fk"
)

// Store owns the connection pool and hands out transactions.
type Store struct {
	pool *pgxpool.Pool
}

// New returns a Store backed by pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Tx is a set of statements running in one database transaction. Every method
// that reads or writes hangs off this type, so no query can accidentally run
// outside a transaction.
type Tx struct {
	tx pgx.Tx
}

// WithTx runs fn inside a single transaction, committing if it returns nil and
// rolling back otherwise.
//
// The Store supplies the mechanism; the caller decides the boundary by choosing
// what goes inside fn. Note that returning nil commits: a transfer that failed
// for insufficient funds is a successful outcome of the transaction, because
// the FAILED row must survive for the idempotency key to keep reporting it.
func (s *Store) WithTx(ctx context.Context, fn func(context.Context, *Tx) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}

	// Rollback after a successful commit is a no-op, so this is safe to defer
	// unconditionally and guarantees no transaction is left open on a panic.
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(ctx, &Tx{tx: tx}); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// pgErrorCode reports the SQLSTATE and constraint name of a PostgreSQL error,
// or empty strings if err did not come from the server.
func pgErrorCode(err error) (code, constraint string) {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code, pgErr.ConstraintName
	}
	return "", ""
}

// asWalletBusy converts a lock timeout into the domain error for it, leaving
// every other error untouched.
//
// A lock timeout means another transfer holds the wallet's row. The request
// never started, so the caller can retry it unchanged.
func asWalletBusy(err error) error {
	if code, _ := pgErrorCode(err); code == sqlstateLockNotAvailable {
		return domain.ErrWalletBusy
	}
	return err
}
