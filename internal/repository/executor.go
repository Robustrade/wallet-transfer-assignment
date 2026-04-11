package repository

import (
	"context"
	"database/sql"
)

// Executor is satisfied by both *sql.DB and *sql.Tx, allowing repository
// methods to be called with or without an active transaction.
type Executor interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}
