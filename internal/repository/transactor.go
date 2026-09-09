package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wallet-transfer-assignment/internal/repository/db"
)

type txContextKey struct{}

// Transactor defines the interface for executing operations within a database transaction.
type Transactor interface {
	WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}

// PgxTransactor implements Transactor using a pgx connection pool.
type PgxTransactor struct {
	pool *pgxpool.Pool
}

// NewTransactor creates a new PgxTransactor.
func NewTransactor(pool *pgxpool.Pool) *PgxTransactor {
	return &PgxTransactor{pool: pool}
}

// WithinTransaction executes fn within a database transaction.
func (t *PgxTransactor) WithinTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	tx, err := t.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	txCtx := context.WithValue(ctx, txContextKey{}, tx)
	if err := fn(txCtx); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}
	return nil
}

// getDBTX returns the active transaction from context if present, otherwise the pool.
func getDBTX(ctx context.Context, pool *pgxpool.Pool) db.DBTX {
	if tx, ok := ctx.Value(txContextKey{}).(pgx.Tx); ok {
		return tx
	}
	return pool
}
