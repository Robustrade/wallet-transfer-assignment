// Package migrate applies embedded SQL migrations and seed data to Postgres.
//
// Each migration runs in its own transaction and is recorded in schema_migrations.
// A session-level advisory lock serialises concurrent runners (e.g. several
// containers starting at once), so every migration is applied exactly once.
package migrate

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// advisoryLockKey is an arbitrary but stable key identifying "schema migration in progress".
const advisoryLockKey int64 = 7_351_204_119

const seedDir = "seed"

// Up applies every *.sql migration at the root of fsys that has not been applied yet,
// in lexical order, and returns the versions it applied.
func Up(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, logger *slog.Logger) ([]string, error) {
	files, err := sqlFiles(fsys, ".")
	if err != nil {
		return nil, err
	}

	var applied []string
	err = withAdvisoryLock(ctx, pool, func(conn *pgxpool.Conn) error {
		if _, err := conn.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`); err != nil {
			return fmt.Errorf("create schema_migrations: %w", err)
		}

		for _, file := range files {
			version := strings.TrimSuffix(path.Base(file), ".sql")
			ran, err := applyOnce(ctx, conn, fsys, file, version)
			if err != nil {
				return err
			}
			if ran {
				applied = append(applied, version)
				logger.InfoContext(ctx, "migration applied", slog.String("version", version))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	logger.InfoContext(ctx, "migrations up to date", slog.Int("applied", len(applied)))
	return applied, nil
}

// Seed applies every seed/*.sql file. Seed files must be idempotent; they run on every call.
func Seed(ctx context.Context, pool *pgxpool.Pool, fsys fs.FS, logger *slog.Logger) error {
	files, err := sqlFiles(fsys, seedDir)
	if err != nil {
		return err
	}
	return withAdvisoryLock(ctx, pool, func(conn *pgxpool.Conn) error {
		for _, file := range files {
			sql, err := fs.ReadFile(fsys, file)
			if err != nil {
				return fmt.Errorf("read seed %s: %w", file, err)
			}
			if _, err := conn.Exec(ctx, string(sql)); err != nil {
				return fmt.Errorf("apply seed %s: %w", file, err)
			}
			logger.InfoContext(ctx, "seed applied", slog.String("file", file))
		}
		return nil
	})
}

// applyOnce runs one migration and records it, atomically. It reports whether it ran.
func applyOnce(ctx context.Context, conn *pgxpool.Conn, fsys fs.FS, file, version string) (bool, error) {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful commit

	var exists bool
	if err := tx.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	if exists {
		return false, nil
	}

	sql, err := fs.ReadFile(fsys, file)
	if err != nil {
		return false, fmt.Errorf("read migration %s: %w", file, err)
	}
	// No arguments, so pgx uses the simple protocol and multi-statement files are allowed.
	if _, err := tx.Exec(ctx, string(sql)); err != nil {
		return false, fmt.Errorf("apply migration %s: %w", version, err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return false, fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", version, err)
	}
	return true, nil
}

// withAdvisoryLock runs fn on a single connection holding the migration advisory lock.
func withAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, fn func(conn *pgxpool.Conn) error) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, advisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Unlock even if ctx was cancelled; the lock is otherwise held until the session ends.
		_, _ = conn.Exec(context.WithoutCancel(ctx), `SELECT pg_advisory_unlock($1)`, advisoryLockKey)
	}()

	return fn(conn)
}

// sqlFiles lists *.sql files directly inside dir, sorted lexically.
func sqlFiles(fsys fs.FS, dir string) ([]string, error) {
	entries, err := fs.ReadDir(fsys, dir)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, path.Join(dir, e.Name()))
		}
	}
	sort.Strings(files)
	return files, nil
}
