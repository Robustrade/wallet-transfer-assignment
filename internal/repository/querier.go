package repository

import (
	"context"
	"database/sql"
)

// Querier is satisfied by both *sql.DB and *sql.Tx. Repositories accept it
// rather than a concrete type so the service layer controls transaction
// boundaries and repositories stay pure persistence code.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
