package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// TxManager wraps a *sql.DB and runs callbacks inside a transaction.
// It is the single transaction primitive used by the service layer;
// repositories themselves never call BeginTx.
type TxManager struct {
	db *sql.DB
}

// NewTxManager returns a TxManager bound to the given *sql.DB. The DB
// instance is the same one the service layer uses for non-tx reads;
// pgx's connection pool handles concurrency.
func NewTxManager(db *sql.DB) *TxManager {
	return &TxManager{db: db}
}

// RunInTx executes fn inside a database transaction.
//
// Lifecycle:
//
// 1. BeginTx is called with the caller's context so cancellation
// propagates into the transaction.
//
// 2. fn is invoked with the *sql.Tx wearing the Executor interface so
// nothing leaks the concrete *sql.Tx type to the service layer.
//
// 3. The deferred closure picks one of three exit paths, in this
// order of precedence:
//
// (a) fn panicked: rollback, then re-panic so the runtime stack
// trace survives. The Recover middleware in internal/handler
// catches the re-raised panic at the request boundary.
//
// (b) fn returned non-nil error: rollback. Preserve the original
// error; only append a rollback error if the rollback itself failed
// with something other than sql.ErrTxDone (which would mean another
// path already finished the tx).
//
// (c) fn returned nil: commit. A commit failure becomes the returned
// error so callers see it.
//
// `err` is a named return value so the deferred closure can mutate it
// — the function signature would not otherwise let the closure
// influence what the caller observes.
func (m *TxManager) RunInTx(ctx context.Context, fn func(repository.Executor) error) (err error) {
	tx, err := m.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() {
		// Path (a): a panic from fn. Roll back and re-panic so the
		// caller's recover (if any) sees the original value.
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		// Path (b): fn returned an error. Roll back; preserve the
		// original error; surface rollback errors only if they are
		// real (sql.ErrTxDone means somebody already finished the tx).
		if err != nil {
			if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
				err = fmt.Errorf("%w; rollback: %v", err, rbErr)
			}
			return
		}
		// Path (c): fn returned nil. Commit; a commit failure becomes
		// the returned error.
		if cmErr := tx.Commit(); cmErr != nil {
			err = fmt.Errorf("commit: %w", cmErr)
		}
	}()
	err = fn(tx)
	return
}
