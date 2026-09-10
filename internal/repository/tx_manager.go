package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// pgTxManager implements TxManager using pgx connection pool.
type pgTxManager struct {
	pool *pgxpool.Pool
}

// NewTxManager creates a new PostgreSQL transaction manager.
func NewTxManager(pool *pgxpool.Pool) TxManager {
	return &pgTxManager{pool: pool}
}

// WithTx executes a function within a database transaction.
// The transaction is committed if fn returns nil, rolled back otherwise.
func (m *pgTxManager) WithTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := m.pool.Begin(ctx)
	if err != nil {
		return err
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(tx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	return tx.Commit(ctx)
}
