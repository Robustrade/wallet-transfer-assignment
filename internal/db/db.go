// Package db holds database connection setup and schema migration. It contains
// no business logic and no queries beyond schema management.
package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// lockTimeout bounds how long a statement will wait for a row lock.
//
// Transfers take pessimistic locks on both wallet rows, so the failure mode to
// guard against is a stuck transaction blocking every other transfer touching
// the same wallet indefinitely. Waiting a bounded time and failing loudly is
// better than a request hanging until the client gives up: the service maps the
// resulting error to a retryable 503.
const lockTimeout = 3 * time.Second

// Open creates a connection pool and verifies it can reach the database.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database dsn: %w", err)
	}

	// Applied to every connection in the pool, so no transaction can forget it.
	cfg.ConnConfig.RuntimeParams["lock_timeout"] = fmt.Sprintf("%d", lockTimeout.Milliseconds())

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create connection pool: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return pool, nil
}
