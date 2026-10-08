// Package postgres implements repository contracts using a transaction-bound sql.Tx.
// The application's database package registers pgx/v5/stdlib and opens the pool.
package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"wallet-transfer-assignment/internal/repository"
)

type Store struct{ db *sql.DB }

func New(db *sql.DB) *Store { return &Store{db: db} }

var _ repository.UnitOfWork = (*Store)(nil)
var _ repository.Tx = (*transaction)(nil)

type transaction struct {
	tx      *sql.Tx
	failure error
}

// record remembers SQL failures so even a mistakenly swallowed error cannot lead
// to commit. Expected misses and guarded-write conflicts do not poison the tx.
func (t *transaction) record(op string, err error) error {
	wrapped := wrap(op, err)
	if errors.Is(wrapped, repository.ErrTransactionFailed) && t.failure == nil {
		t.failure = wrapped
	}
	return wrapped
}

func (s *Store) WithinTransaction(ctx context.Context, fn func(context.Context, repository.Tx) error) (err error) {
	if fn == nil {
		return fmt.Errorf("transaction callback is required")
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		return wrap("begin transaction", err)
	}
	defer func() {
		// Also runs during panic unwinding; do not recover the callback's panic.
		rollbackErr := tx.Rollback()
		if rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			err = errors.Join(err, wrap("rollback transaction", rollbackErr))
		}
	}()
	scoped := &transaction{tx: tx}
	if err = fn(ctx, scoped); err != nil {
		return fmt.Errorf("transaction callback: %w", err)
	}
	if scoped.failure != nil {
		return scoped.failure
	}
	if err = ctx.Err(); err != nil {
		return wrap("transaction canceled", err)
	}
	return wrap("commit transaction", tx.Commit())
}

func (t *transaction) exec(ctx context.Context, op, query string, args ...any) (sql.Result, error) {
	if t.failure != nil {
		return nil, t.failure
	}
	result, err := t.tx.ExecContext(ctx, query, args...)
	return result, t.record(op, err)
}
func (t *transaction) update(ctx context.Context, op, query string, missing error, args ...any) error {
	result, err := t.exec(ctx, op, query, args...)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err != nil {
		return t.record(op, err)
	}
	if n == 0 {
		return fmt.Errorf("%s: %w", op, missing)
	}
	return nil
}
