// Package postgres provides PostgreSQL adapters implementing the service ports.
package postgres

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/service"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// DB owns the connection pool and is the single entry point for repositories
// and transaction management. Repositories hold a *DB and resolve the active
// connection (pool or ambient transaction) through conn.
type DB struct {
	pool *pgxpool.Pool
}

// NewDB opens a pgx connection pool for dsn and verifies connectivity.
func NewDB(ctx context.Context, dsn string) (*DB, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, fmt.Errorf("postgres: new pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("postgres: ping: %w", err)
	}
	return &DB{pool: pool}, nil
}

// Close releases the underlying connection pool.
func (db *DB) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// Pool exposes the underlying pool for callers that need direct access (for
// example, health checks). Repositories should use conn instead.
func (db *DB) Pool() *pgxpool.Pool {
	return db.pool
}

// Querier is the subset of methods shared by *pgxpool.Pool and pgx.Tx.
// Repositories depend on this interface so the same query code runs both
// inside and outside an ambient transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// txKey is the private context key under which an active transaction is stored.
type txKeyType struct{}

var txKey = txKeyType{}

// withTx returns a child context carrying tx.
func withTx(ctx context.Context, tx pgx.Tx) context.Context {
	return context.WithValue(ctx, txKey, tx)
}

// txFromContext returns the transaction carried on ctx, if any.
func txFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(txKey).(pgx.Tx)
	return tx, ok
}

// conn returns the ambient transaction from ctx when present, otherwise the
// pool. Call sites never thread a *tx by hand.
func (db *DB) conn(ctx context.Context) Querier {
	if tx, ok := txFromContext(ctx); ok {
		return tx
	}
	return db.pool
}

// Compile-time assertion that DB satisfies the TxManager port.
var _ service.TxManager = (*DB)(nil)

// WithinTx runs fn inside a single transaction. The transaction is placed on
// the context passed to fn so repositories participate automatically. It
// commits on a nil error, rolls back otherwise, and rolls back then re-panics
// if fn panics.
func (db *DB) WithinTx(ctx context.Context, fn func(ctx context.Context) error) (err error) {
	// A nested call reuses the already-active transaction so savepoint
	// semantics are not silently introduced.
	if tx, ok := txFromContext(ctx); ok {
		_ = tx
		return fn(ctx)
	}

	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin tx: %w", err)
	}

	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	if err := fn(withTx(ctx, tx)); err != nil {
		if rbErr := tx.Rollback(ctx); rbErr != nil && !errors.Is(rbErr, pgx.ErrTxClosed) {
			return fmt.Errorf("%w (rollback: %v)", err, rbErr)
		}
		return err
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit tx: %w", err)
	}
	return nil
}

// Migrate creates the schema_migrations bookkeeping table and applies every
// embedded migration that has not yet been recorded, each in its own
// transaction. It is idempotent across restarts.
func (db *DB) Migrate(ctx context.Context) error {
	if _, err := db.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("postgres: create schema_migrations: %w", err)
	}

	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("postgres: read migrations dir: %w", err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sql") {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)

	for _, name := range names {
		if err := db.applyMigration(ctx, name); err != nil {
			return err
		}
	}
	return nil
}

// applyMigration applies a single migration file within its own transaction,
// skipping it if already recorded.
func (db *DB) applyMigration(ctx context.Context, name string) error {
	var exists bool
	if err := db.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, name,
	).Scan(&exists); err != nil {
		return fmt.Errorf("postgres: check migration %q: %w", name, err)
	}
	if exists {
		return nil
	}

	sqlBytes, err := migrationsFS.ReadFile("migrations/" + name)
	if err != nil {
		return fmt.Errorf("postgres: read migration %q: %w", name, err)
	}

	tx, err := db.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("postgres: begin migration %q: %w", name, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("postgres: apply migration %q: %w", name, err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, name,
	); err != nil {
		return fmt.Errorf("postgres: record migration %q: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("postgres: commit migration %q: %w", name, err)
	}
	return nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique-constraint
// violation (SQLSTATE 23505).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
