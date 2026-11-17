package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ctxKey struct{}

// Querier is the subset of pgx pool/tx used by repositories.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// WithTx stores tx on ctx so repositories use the service-owned transaction.
func WithTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, ctxKey{}, tx)
}

// QuerierFrom returns the transaction from ctx, or the pool when no tx is present.
func QuerierFrom(ctx context.Context, pool *pgxpool.Pool) Querier {
	if tx, ok := ctx.Value(ctxKey{}).(pgx.Tx); ok && tx != nil {
		return tx
	}
	return pool
}

// BeginTx starts a transaction and returns a context that carries it.
func BeginTx(ctx context.Context, pool *pgxpool.Pool) (context.Context, pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return ctx, nil, fmt.Errorf("begin tx: %w", err)
	}
	return WithTx(ctx, tx), tx, nil
}
