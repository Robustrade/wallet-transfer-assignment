package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// migrationLockID namespaces the advisory lock Migrate takes. Any constant
// works as long as it is unique within the database; this one is arbitrary.
const migrationLockID int64 = 8274135

// unlockTimeout bounds the advisory unlock that runs as Migrate unwinds. It is
// short because the statement is trivial, and bounded so a wedged connection
// cannot hold up a shutdown that is already in progress.
const unlockTimeout = 5 * time.Second

// Migrate applies every unapplied migration in source, in lexical filename
// order, and records each one so a second call is a no-op.
//
// Migrations are forward-only: this is a service whose schema is versioned in
// git, and a hand-written rollback is more likely to be wrong than useful. Each
// migration runs in its own transaction alongside the row recording it, so a
// failure leaves neither a half-applied schema nor a false record of success.
//
// A session-level advisory lock serializes concurrent callers, so several
// service instances starting at once cannot race each other.
func Migrate(ctx context.Context, pool *pgxpool.Pool, source fs.FS) error {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire connection for migration: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationLockID); err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer func() {
		// Unlocked on a context of its own, because ctx may be the reason we are
		// unwinding: pgx sends nothing on a cancelled context, so reusing it here
		// would skip the unlock entirely.
		//
		// Releasing the connection does not cover for that. An advisory lock is
		// session-scoped and Release returns the connection to the pool rather
		// than closing it, so the lock rides back into the pool still held. Today
		// every caller exits the process when a migration fails, and the dying
		// connection is what actually frees the lock — but a caller that
		// cancelled and kept running would leave it held, and the next migration
		// on a different connection would wait for it forever.
		unlockCtx, cancel := context.WithTimeout(context.Background(), unlockTimeout)
		defer cancel()

		_, _ = conn.Exec(unlockCtx, `SELECT pg_advisory_unlock($1)`, migrationLockID)
	}()

	if _, err := conn.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version     TEXT        PRIMARY KEY,
			applied_at  TIMESTAMPTZ NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, conn)
	if err != nil {
		return err
	}

	versions, err := availableVersions(source)
	if err != nil {
		return err
	}

	for _, version := range versions {
		if applied[version] {
			continue
		}

		statements, err := fs.ReadFile(source, version)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		if err := applyMigration(ctx, conn, version, string(statements)); err != nil {
			return err
		}
	}

	return nil
}

// applyMigration runs one migration and records it in the same transaction, so
// the schema and the record of it can never disagree.
func applyMigration(ctx context.Context, conn *pgxpool.Conn, version, statements string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, statements); err != nil {
		return fmt.Errorf("apply migration %s: %w", version, err)
	}

	if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
		return fmt.Errorf("record migration %s: %w", version, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", version, err)
	}

	return nil
}

func appliedVersions(ctx context.Context, conn *pgxpool.Conn) (map[string]bool, error) {
	rows, err := conn.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		applied[version] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}

	return applied, nil
}

func availableVersions(source fs.FS) ([]string, error) {
	entries, err := fs.Glob(source, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list migrations: %w", err)
	}
	if len(entries) == 0 {
		return nil, errors.New("no migrations found")
	}

	for i, entry := range entries {
		entries[i] = path.Base(entry)
	}
	sort.Strings(entries)

	return entries, nil
}
