// Package postgres implements the service persistence ports on top of
// PostgreSQL using pgx. Driver errors are translated to domain errors here so
// nothing above the repository boundary depends on pgx.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// SQLSTATE codes we treat specially.
const (
	uniqueViolation      = "23505"
	serializationFailure = "40001"
	deadlockDetected     = "40P01"
)

// maxTxRetries bounds how often WithinTx replays a transaction that failed with
// a transient serialization error.
const maxTxRetries = 3

// querier is the common surface of *pgxpool.Pool and pgx.Tx.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type txCtxKey struct{}

// DB owns the connection pool and hands repositories the right querier: the
// active transaction if the call is inside TxManager.WithinTx, otherwise the
// pool.
type DB struct {
	pool *pgxpool.Pool
}

// Connect opens and verifies a pool.
func Connect(ctx context.Context, dsn string, maxConns int32, connectTimeout time.Duration) (*DB, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: parse dsn: %w", err)
	}
	if maxConns > 0 {
		cfg.MaxConns = maxConns
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("postgres: create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Pool exposes the underlying pool (migrations, health checks).
func (db *DB) Pool() *pgxpool.Pool { return db.pool }

// Ping verifies the database is reachable.
func (db *DB) Ping(ctx context.Context) error { return db.pool.Ping(ctx) }

// Close releases all connections.
func (db *DB) Close() { db.pool.Close() }

// q returns the querier for this call.
func (db *DB) q(ctx context.Context) querier {
	if tx, ok := ctx.Value(txCtxKey{}).(pgx.Tx); ok {
		return tx
	}
	return db.pool
}

// WithinTx runs fn inside a single transaction. Repositories called with the
// context passed to fn use that transaction. A transaction that fails with a
// deadlock or serialization error is retried a bounded number of times, since
// fn is expected to be a pure retry of the same logical operation.
func (db *DB) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	var err error
	for attempt := 1; ; attempt++ {
		err = db.runTx(ctx, fn)
		if err == nil || attempt >= maxTxRetries || !isTransient(err) {
			return err
		}
	}
}

func (db *DB) runTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	tx, err := db.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("postgres: begin: %w", err)
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
		if err != nil {
			if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
				err = errors.Join(err, fmt.Errorf("rollback: %w", rbErr))
			}
			return
		}
		if cErr := tx.Commit(ctx); cErr != nil {
			err = fmt.Errorf("postgres: commit: %w", cErr)
		}
	}()

	return fn(context.WithValue(ctx, txCtxKey{}, tx))
}

// isUniqueViolation reports whether err is a Postgres unique-constraint error,
// optionally on a specific constraint name.
func isUniqueViolation(err error, constraint string) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != uniqueViolation {
		return false
	}
	return constraint == "" || pgErr.ConstraintName == constraint
}

// isTransient reports whether err is a retryable serialization failure.
func isTransient(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == serializationFailure || pgErr.Code == deadlockDetected
}
