package sqlrepo

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"time"
)

// seedSuffix marks migrations that insert demo data rather than change the
// schema. They run only when Migrate is asked to seed.
const seedSuffix = "_seed.sql"

// Migrate applies every *.sql file in migrations that has not been applied
// yet, in lexical order (0001_..., 0002_...). Each file runs in its own
// transaction, together with the row that records it in schema_migrations,
// so a file is applied completely or not at all.
//
// Scope: this is a deliberately small runner. It assumes one process
// migrates at a time. Running several replicas would need an advisory lock
// or a dedicated migration step.
func Migrate(ctx context.Context, db *sql.DB, d Dialect, migrations fs.FS, includeSeed bool) error {
	if _, err := db.ExecContext(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (version TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	names, err := fs.Glob(migrations, "*.sql")
	if err != nil {
		return err
	}
	sort.Strings(names)

	for _, name := range names {
		if strings.HasSuffix(name, seedSuffix) && !includeSeed {
			continue
		}
		if err := applyMigration(ctx, db, d, migrations, name); err != nil {
			return fmt.Errorf("migration %s: %w", name, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, d Dialect, migrations fs.FS, name string) error {
	var one int
	err := db.QueryRowContext(ctx, d.Rebind(`SELECT 1 FROM schema_migrations WHERE version = ?`), name).Scan(&one)
	if err == nil {
		return nil // already applied
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}

	body, err := fs.ReadFile(migrations, name)
	if err != nil {
		return err
	}

	tx, err := db.BeginTx(ctx, d.TxOptions())
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }() // no-op after a successful Commit

	// Both drivers accept several ';'-separated statements in one Exec
	// call without arguments.
	if _, err := tx.ExecContext(ctx, string(body)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		d.Rebind(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`),
		name, FormatTime(time.Now())); err != nil {
		return err
	}
	return tx.Commit()
}
