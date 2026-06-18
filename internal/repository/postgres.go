package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore holds the connection pool and implements TxManager.
type PostgresStore struct {
	pool *pgxpool.Pool
}

// NewPostgresStore opens a connection pool to the given DSN.
func NewPostgresStore(ctx context.Context, dsn string) (*PostgresStore, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}
	return &PostgresStore{pool: pool}, nil
}

// Close releases the pool.
func (s *PostgresStore) Close() {
	s.pool.Close()
}

// WithTx runs fn inside a single transaction.
// Rolls back on error or panic, commits otherwise.
func (s *PostgresStore) WithTx(ctx context.Context, fn func(repo Repository) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}

	defer func() {
		// Safe to call after commit; pgx treats it as no-op.
		_ = tx.Rollback(ctx)
	}()

	repo := &pgRepository{tx: tx}
	if err := fn(repo); err != nil {
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// pgRepository implements Repository bound to a single tx.
type pgRepository struct {
	tx pgx.Tx
}