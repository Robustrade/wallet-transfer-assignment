// Package postgres bootstraps the pgxpool connection pool. It has no
// domain knowledge — see docs/DESIGN.md §8. Callers are responsible for
// pinging the returned pool before relying on it.
package postgres

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// New constructs a connection pool for dsn. It does not verify
// connectivity — call Ping on the result to fail fast.
func New(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("create postgres pool: %w", err)
	}
	return pool, nil
}
