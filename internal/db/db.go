// Package db handles database connection setup and schema migrations.
//
// The package is the boundary between the application's *sql.DB and
// the actual Postgres driver / migration runner. Everything above
// (repository, service, handler) treats the database as a black box
// behind the database/sql interface.
package db

import (
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/golang-migrate/migrate/v4"
	migratepg "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"

	// Register the pgx driver for database/sql under the name "pgx".
	// We import for side effects only; the package's init() function
	// calls sql.Register("pgx", ...). Without this blank import
	// sql.Open("pgx", ...) would return "unknown driver".
	_ "github.com/jackc/pgx/v5/stdlib"
)

// migrationsFS embeds the SQL migration files into the compiled
// binary. The same files also live in the top-level migrations/
// directory for developer-facing tools (psql, migrate CLI); the
// authoritative copy used by db.Migrate is the embedded one.
//
// AGENTS.md §S-12 reminds future contributors to keep both copies in
// sync; the migration skill at .claude/skills/wallet-migration walks
// through the standard add-a-migration workflow.
//
//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open opens a *sql.DB against the given DSN using the pgx driver.
//
// It does a Ping immediately so a misconfigured DSN or an unreachable
// host produces a synchronous error at startup rather than a delayed
// error on the first query — much friendlier when debugging a
// deployment.
//
// The connection pool is left at pgx's defaults; callers that want
// to bound the pool should pass a non-nil PoolConfig via OpenWithPool.
// Both helpers are intentionally separate: Open keeps test setup
// trivial, OpenWithPool is what production wiring uses.
func Open(dsn string) (*sql.DB, error) {
	return OpenWithPool(dsn, PoolConfig{})
}

// PoolConfig captures the four database/sql pool knobs that matter
// for production capacity. A zero-value PoolConfig leaves the pool at
// pgx defaults (unbounded MaxOpenConns, etc.) — appropriate for
// tests but NOT for production.
//
// In production, populate every field. See internal/config for
// recommended defaults; see README "Concurrency and scale" for the
// rationale behind each value.
type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// OpenWithPool opens the database and applies the given pool tuning
// before pinging. Apply order matters: we set the limits before the
// first Ping so that Ping itself respects them (it borrows a
// connection from the pool).
func OpenWithPool(dsn string, pool PoolConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	if pool.MaxOpenConns > 0 {
		db.SetMaxOpenConns(pool.MaxOpenConns)
	}
	if pool.MaxIdleConns > 0 {
		db.SetMaxIdleConns(pool.MaxIdleConns)
	}
	if pool.ConnMaxLifetime > 0 {
		db.SetConnMaxLifetime(pool.ConnMaxLifetime)
	}
	if pool.ConnMaxIdleTime > 0 {
		db.SetConnMaxIdleTime(pool.ConnMaxIdleTime)
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("db.Ping: %w", err)
	}
	return db, nil
}

// Migrate applies all up-migrations embedded in this binary.
//
// The runner is golang-migrate. It tracks applied migrations in a
// schema_migrations table that it creates automatically on first run.
// migrate.ErrNoChange is the "everything up to date" sentinel; we
// swallow it because successful startup with no pending migrations
// is the normal case.
//
// Any other error from Up() means the migration failed and the
// caller (cmd/server.run) should refuse to start.
func Migrate(db *sql.DB) error {
	src, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("iofs source: %w", err)
	}
	driver, err := migratepg.WithInstance(db, &migratepg.Config{})
	if err != nil {
		return fmt.Errorf("migrate driver: %w", err)
	}
	m, err := migrate.NewWithInstance("iofs", src, "postgres", driver)
	if err != nil {
		return fmt.Errorf("migrate new: %w", err)
	}
	if err := m.Up(); err != nil && err != migrate.ErrNoChange {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
