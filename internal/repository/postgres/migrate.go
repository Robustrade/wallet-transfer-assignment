package postgres

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// Migrator applies embedded ".up.sql" migrations in filename order, recording
// applied versions in a schema_migrations table. It is intentionally minimal —
// forward-only, one file per version — which is all this service needs. For
// richer workflows use the golang-migrate CLI against internal/repository/migrations.
type Migrator struct {
	db   *DB
	fsys fs.FS
}

// NewMigrator wires a Migrator.
func NewMigrator(db *DB, fsys fs.FS) *Migrator {
	return &Migrator{db: db, fsys: fsys}
}

// Up applies every pending migration. It is safe to call on every startup.
func (m *Migrator) Up(ctx context.Context) error {
	if _, err := m.db.pool.Exec(ctx,
		`CREATE TABLE IF NOT EXISTS schema_migrations (
		     version    TEXT PRIMARY KEY,
		     applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		 )`); err != nil {
		return fmt.Errorf("postgres: ensure schema_migrations: %w", err)
	}

	applied, err := m.appliedVersions(ctx)
	if err != nil {
		return err
	}

	files, err := m.upFiles()
	if err != nil {
		return err
	}

	for _, f := range files {
		version := strings.TrimSuffix(f, ".up.sql")
		if applied[version] {
			continue
		}
		body, err := fs.ReadFile(m.fsys, f)
		if err != nil {
			return fmt.Errorf("postgres: read migration %s: %w", f, err)
		}
		if err := m.applyOne(ctx, version, string(body)); err != nil {
			return err
		}
	}
	return nil
}

func (m *Migrator) applyOne(ctx context.Context, version, sql string) error {
	return m.db.WithinTx(ctx, func(ctx context.Context) error {
		if _, err := m.db.q(ctx).Exec(ctx, sql); err != nil {
			return fmt.Errorf("postgres: apply migration %s: %w", version, err)
		}
		if _, err := m.db.q(ctx).Exec(ctx,
			`INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			return fmt.Errorf("postgres: record migration %s: %w", version, err)
		}
		return nil
	})
}

func (m *Migrator) appliedVersions(ctx context.Context) (map[string]bool, error) {
	rows, err := m.db.pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("postgres: load applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		applied[v] = true
	}
	return applied, rows.Err()
}

func (m *Migrator) upFiles() ([]string, error) {
	entries, err := fs.ReadDir(m.fsys, ".")
	if err != nil {
		return nil, fmt.Errorf("postgres: read migrations dir: %w", err)
	}
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".up.sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}
