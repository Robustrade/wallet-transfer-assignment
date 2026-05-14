// Package db provides the database connection pool and a shared DBTX interface
// that is satisfied by both *pgxpool.Pool and pgx.Tx. This allows repository
// methods to work with either a pool (for simple queries) or an in-progress
// transaction (for atomic operations), without coupling repositories to a
// specific connection type.
package db

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is the minimal interface shared by *pgxpool.Pool and pgx.Tx.
// Repository methods accept this type so they can participate in transactions
// without being aware of the transaction lifecycle.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// NewPool creates and verifies a pgxpool connection to the given DSN.
func NewPool(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}
