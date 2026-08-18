package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TxManager abstracts starting a DB transaction so the service layer can
// orchestrate multi-step work (lock wallets, write ledger, update state)
// as a single atomic unit without knowing about pgx directly.
type TxManager interface {
	// WithinTx runs fn inside a DB transaction. If fn returns an error,
	// the transaction is rolled back; otherwise it's committed.
	WithinTx(ctx context.Context, fn func(tx pgx.Tx) error) error
}

type pgxTxManager struct {
	pool *pgxpool.Pool
}

func NewTxManager(pool *pgxpool.Pool) TxManager {
	return &pgxTxManager{pool: pool}
}

// WithinTx starts a transaction, runs fn, and commits or rolls back based
// on whether fn returns an error. This is the single place transaction
// boundaries are decided — the service layer calls this and doesn't touch
// BEGIN/COMMIT/ROLLBACK directly.
// Need to make sure every transaction is ACID Commpliant
func (m *pgxTxManager) WithinTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("beginning transaction: %w", err)
	}

	// Defer a rollback that's a no-op if the tx was already committed.
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if err := fn(tx); err != nil {
		return err // rollback happens via defer
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("committing transaction: %w", err)
	}

	return nil
}
