package repository

import (
	"context"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// DBTX is the minimal subset of pgx's pool/transaction API the repositories
// need. Accepting this interface (rather than a concrete *pgxpool.Pool)
// lets the same repository methods run either against the connection pool
// directly or against an in-flight transaction (pgx.Tx), which is how the
// service layer enforces its transaction boundaries without repositories
// knowing or deciding anything about when a transaction starts or ends.
type DBTX interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}
