package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DBTX is implemented by both pgxpool.Pool and pgx.Tx. Repositories can
// therefore execute the same SQL either outside a transaction or inside the
// transfer transaction.
type DBTX interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

// Keep pgxpool.Pool referenced here so the intended production transaction
// entry point is explicit.
var _ DBTX = (*pgxpool.Pool)(nil)
var _ DBTX = (pgx.Tx)(nil)
