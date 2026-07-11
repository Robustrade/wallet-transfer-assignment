// Package postgres implements the persistence layer against real
// Postgres via pgx/v5. No business rules live here — only how to
// lock/read/write a row. Whether a balance is sufficient, or a wallet
// pair is valid, is a decision for domain/service, not this package.
package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository"
)

// Transactor begins transactions on a *pgxpool.Pool, adapting it to
// repository.Transactor so internal/service can own the transaction
// boundary without depending on pgxpool directly.
type Transactor struct {
	pool *pgxpool.Pool
}

// NewTransactor constructs a Transactor for pool.
func NewTransactor(pool *pgxpool.Pool) *Transactor {
	return &Transactor{pool: pool}
}

// Begin starts a new transaction.
func (t *Transactor) Begin(ctx context.Context) (repository.Tx, error) {
	return t.pool.Begin(ctx)
}
