// Package repository contains persistence operations for wallets, transfers,
// ledger entries, and idempotency records.
//
// Store only wires the connection pool; the query/locking logic that
// implements the transfer design lives here as it's written.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps the shared database connection pool used by repository methods.
type Store struct {
	Pool *pgxpool.Pool
}

// NewStore constructs a Store around an existing pgx connection pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{Pool: pool}
}

// Executor is satisfied by both *pgxpool.Pool and pgx.Tx, letting repository
// methods run either standalone or as part of an in-flight transaction.
type Executor interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func (s *Store) WithinTx(ctx context.Context, fn func(tx pgx.Tx) error) (err error) {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tx.Rollback(ctx)
		} else {
			err = tx.Commit(ctx)
		}
	}()
	return fn(tx)
}
