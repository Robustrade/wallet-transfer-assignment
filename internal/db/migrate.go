package db

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.up.sql
var migrationFiles embed.FS

// migrationLockKey is an arbitrary, fixed advisory-lock key scoped to
// migrations. Any int64 works as long as it's stable across processes and
// not reused for an unrelated lock elsewhere in the app (nothing else in
// this codebase takes an advisory lock).
const migrationLockKey = 7_262_025

// Migrate applies every embedded *.up.sql migration that hasn't already run,
// in filename order, tracking progress in a schema_migrations table so it is
// safe to call on every process startup.
//
// Startup can race: multiple instances of this service may run Migrate
// concurrently against the same database (e.g. several replicas starting at
// once). A plain "check schema_migrations, then insert" is not safe under
// that race -- two processes can both see a migration as unapplied before
// either commits, and the loser's INSERT fails on the primary key. To avoid
// that, the whole migration run is wrapped in a Postgres session-level
// advisory lock, held on a single dedicated connection, so only one process
// is ever actually running migrations at a time; any concurrent caller just
// blocks until the lock is free, then finds every migration already applied
// and returns immediately.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for migrations: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, int64(migrationLockKey)); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Use a background context: ctx may already be cancelled/timed out
		// by the time we get here, but the lock must still be released.
		if _, err := conn.Exec(context.Background(), `SELECT pg_advisory_unlock($1)`, int64(migrationLockKey)); err != nil {
			slog.Error("release migration lock", "error", err)
		}
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	entries, err := fs.Glob(migrationFiles, "migrations/*.up.sql")
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	sort.Strings(entries)

	for _, path := range entries {
		version := strings.TrimSuffix(strings.TrimPrefix(path, "migrations/"), ".up.sql")

		var alreadyApplied bool
		if err := conn.QueryRow(ctx,
			`SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
		).Scan(&alreadyApplied); err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if alreadyApplied {
			continue
		}

		sqlBytes, err := migrationFiles.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", path, err)
		}

		tx, err := conn.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for migration %s: %w", version, err)
		}

		if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
		if _, err := tx.Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1) ON CONFLICT (version) DO NOTHING`, version,
		); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", version, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", version, err)
		}
	}

	return nil
}
