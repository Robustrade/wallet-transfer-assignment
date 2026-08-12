package postgres

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// migrationAdvisoryLockKey is an arbitrary constant used with
// pg_advisory_lock so that concurrent process startups can't race to apply
// the same migration twice.
const migrationAdvisoryLockKey int64 = 0x77616c6c6574 // "wallet" in hex, just needs to be a fixed constant

type migration struct {
	version int
	name    string
	upSQL   string
}

// loadMigrations reads every *.up.sql file embedded at build time and
// returns them sorted in ascending version order. Down migrations are
// shipped alongside for operator use but are never applied programmatically.
func loadMigrations() ([]migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("read migrations dir: %w", err)
	}

	var migrations []migration
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		versionStr, _, _ := strings.Cut(name, "_")
		version, err := strconv.Atoi(versionStr)
		if err != nil {
			return nil, fmt.Errorf("migration file %q does not start with a numeric version: %w", name, err)
		}
		contents, err := fs.ReadFile(migrationsFS, "migrations/"+name)
		if err != nil {
			return nil, fmt.Errorf("read migration %q: %w", name, err)
		}
		migrations = append(migrations, migration{
			version: version,
			name:    strings.TrimSuffix(name, ".up.sql"),
			upSQL:   string(contents),
		})
	}

	sort.Slice(migrations, func(i, j int) bool { return migrations[i].version < migrations[j].version })
	return migrations, nil
}

// Migrate applies every embedded migration that has not yet been recorded in
// schema_migrations, in ascending version order, each in its own
// transaction. It is safe to call concurrently from multiple process starts:
// a Postgres advisory lock serializes the whole operation.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	migrations, err := loadMigrations()
	if err != nil {
		return err
	}

	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for migration lock: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration advisory lock: %w", err)
	}
	defer func() {
		_, _ = conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", migrationAdvisoryLockKey)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    INTEGER PRIMARY KEY,
			name       TEXT NOT NULL,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[int]bool{}
	rows, err := conn.Query(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}

	for _, m := range migrations {
		if applied[m.version] {
			continue
		}
		if err := applyMigration(ctx, conn, m); err != nil {
			return fmt.Errorf("apply migration %s: %w", m.name, err)
		}
	}

	return nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, m migration) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, m.upSQL); err != nil {
		return fmt.Errorf("exec up.sql: %w", err)
	}
	if _, err := tx.Exec(ctx,
		"INSERT INTO schema_migrations (version, name) VALUES ($1, $2)",
		m.version, m.name,
	); err != nil {
		return fmt.Errorf("record migration: %w", err)
	}

	return tx.Commit(ctx)
}
