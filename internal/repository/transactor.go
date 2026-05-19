package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PgxTransactor adapts a pgxpool to the transaction boundary the service needs.
// It satisfies the service's Transactor interface structurally, so the service
// package owns that interface without the repository depending on it.
type PgxTransactor struct {
	pool *pgxpool.Pool
}

func NewPgxTransactor(pool *pgxpool.Pool) *PgxTransactor {
	return &PgxTransactor{pool: pool}
}

// DB returns the pool as a non-transactional handle for fast-path reads and
// the pre-write existence checks (which must not open a transaction).
func (t *PgxTransactor) DB() DBTX { return t.pool }

// WithTx runs fn in a single committed transaction (see WithTx).
func (t *PgxTransactor) WithTx(ctx context.Context, fn func(DBTX) error) error {
	return WithTx(ctx, t.pool, func(tx pgx.Tx) error { return fn(tx) })
}
