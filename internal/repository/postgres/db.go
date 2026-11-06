// Package postgres implements the service persistence ports on PostgreSQL.
// It contains SQL and error translation only; no business decisions.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// Postgres error codes we translate.
const (
	codeLockNotAvailable = "55P03" // lock_timeout exceeded
	codeDeadlockDetected = "40P01"
)

// querier is satisfied by both *pgxpool.Pool and pgx.Tx, so repositories work
// inside and outside a transaction.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Options tunes transaction behavior.
type Options struct {
	// LockTimeout bounds how long a statement waits for a row lock. Zero means no limit.
	LockTimeout time.Duration
}

// DB is the Postgres-backed service.Database.
type DB struct {
	repos
	pool *pgxpool.Pool
	opts Options
}

var _ service.Database = (*DB)(nil)

// New returns a Database using pool.
func New(pool *pgxpool.Pool, opts Options) *DB {
	return &DB{repos: repos{q: pool}, pool: pool, opts: opts}
}

// InTx implements service.Database.
func (d *DB) InTx(ctx context.Context, fn func(ctx context.Context, tx service.Store) error) error {
	tx, err := d.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return translate(fmt.Errorf("begin transaction: %w", err))
	}
	// Roll back on any early return or panic; a no-op after a successful commit.
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	if d.opts.LockTimeout > 0 {
		// set_config(..., true) is SET LOCAL: it lasts only for this transaction.
		ms := fmt.Sprintf("%dms", d.opts.LockTimeout.Milliseconds())
		if _, err := tx.Exec(ctx, `SELECT set_config('lock_timeout', $1, true)`, ms); err != nil {
			return translate(fmt.Errorf("set lock_timeout: %w", err))
		}
	}

	if err := fn(ctx, repos{q: tx}); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return translate(fmt.Errorf("commit transaction: %w", err))
	}
	return nil
}

// repos hands out repositories bound to one querier (the pool or a transaction).
type repos struct{ q querier }

func (r repos) Wallets() service.WalletRepository          { return walletRepo(r) }
func (r repos) Transfers() service.TransferRepository      { return transferRepo(r) }
func (r repos) Ledger() service.LedgerRepository           { return ledgerRepo(r) }
func (r repos) Idempotency() service.IdempotencyRepository { return idempotencyRepo(r) }

// translate maps retryable lock failures to domain.ErrBusy and leaves other errors as they are.
func translate(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && (pgErr.Code == codeLockNotAvailable || pgErr.Code == codeDeadlockDetected) {
		return fmt.Errorf("%w: %w", domain.ErrBusy, err)
	}
	return err
}
