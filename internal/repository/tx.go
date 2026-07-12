// Package repository holds contracts shared between the postgres and cache
// implementations and their service-layer consumers. Locking and querying
// are mechanical concerns that live here and in its subpackages — the
// DECISION of what to do with a locked row (e.g. what counts as
// insufficient balance) belongs to domain/service, not here.
package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Querier is the subset of pgx.Tx (and *pgxpool.Pool) needed to run SQL.
type Querier interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Tx is a database transaction, as seen by the repository and service
// layers. Concrete pgx.Tx values satisfy this directly.
type Tx interface {
	Querier
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

// Transactor begins a new transaction. Implemented by postgres.Transactor,
// wrapping *pgxpool.Pool — the transaction boundary itself is owned by
// internal/service, which decides when to commit or roll back.
type Transactor interface {
	Begin(ctx context.Context) (Tx, error)
}
