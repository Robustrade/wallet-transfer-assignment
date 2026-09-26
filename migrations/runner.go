package migrations

import (
	"context"
	"fmt"
	"io/fs"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"wallet/internal/constants"
)

// DBTX is the subset of pgx used by Apply. Both *pgx.Conn and *pgxpool.Conn
// satisfy it.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Apply runs every embedded migration that has not been applied yet and
// returns the versions it applied, in order.
//
// A session-level advisory lock is held for the whole run so that two
// concurrent runners (e.g. two instances starting at once) cannot interleave
// schema changes. Each migration is applied in its own transaction together
// with its schema_migrations row, so a failure leaves neither a partial schema
// nor a false "applied" mark.
func Apply(ctx context.Context, db DBTX) ([]string, error) {
	lockCtx, cancel := context.WithTimeout(ctx, constants.MigrationLockWait)
	_, err := db.Exec(lockCtx, constants.MigrationAdvisoryLockSQL, constants.AdvisoryLockKey)
	cancel()
	if err != nil {
		return nil, fmt.Errorf("acquire migration advisory lock (waited up to %s; another runner may be stuck): %w",
			constants.MigrationLockWait, err)
	}
	defer func() {
		// Release with an uncancellable context: the lock must be dropped even
		// if the caller's context was cancelled mid-run.
		_, _ = db.Exec(context.WithoutCancel(ctx), constants.MigrationAdvisoryUnlockSQL, constants.AdvisoryLockKey)
	}()

	if _, err := db.Exec(ctx, constants.CreateSchemaMigrationsTableSQL); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, db)
	if err != nil {
		return nil, err
	}

	files, err := migrationFiles()
	if err != nil {
		return nil, err
	}

	var ranStable []string
	for _, name := range files {
		if applied[name] {
			continue
		}
		if err := applyOne(ctx, db, name); err != nil {
			return ranStable, err
		}
		ranStable = append(ranStable, name)
	}
	return ranStable, nil
}

func appliedVersions(ctx context.Context, db DBTX) (map[string]bool, error) {
	rows, err := db.Query(ctx, constants.SelectSchemaMigrationVersionsSQL)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func migrationFiles() ([]string, error) {
	entries, err := fs.Glob(FS, "*.sql")
	if err != nil {
		return nil, fmt.Errorf("list embedded migrations: %w", err)
	}
	sort.Strings(entries)
	return entries, nil
}

func applyOne(ctx context.Context, db DBTX, name string) error {
	sqlBytes, err := FS.ReadFile(name)
	if err != nil {
		return fmt.Errorf("read migration %s: %w", name, err)
	}

	tx, err := db.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin migration %s: %w", name, err)
	}
	defer func() { _ = tx.Rollback(context.WithoutCancel(ctx)) }()

	// pgx sends argument-less Exec over the simple protocol, which allows the
	// whole file (multiple statements) in one call.
	if _, err := tx.Exec(ctx, string(sqlBytes)); err != nil {
		return fmt.Errorf("apply migration %s: %w", name, err)
	}
	if _, err := tx.Exec(ctx, constants.RecordSchemaMigrationVersionSQL, name); err != nil {
		return fmt.Errorf("record migration %s: %w", name, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit migration %s: %w", name, err)
	}
	return nil
}
