package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

type PoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
}

func Open(ctx context.Context, databaseURL string, cfg PoolConfig) (*sql.DB, error) {
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	db.SetMaxOpenConns(cfg.MaxOpenConns)
	db.SetMaxIdleConns(cfg.MaxIdleConns)
	db.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return db, nil
}

func Migrate(ctx context.Context, db *sql.DB, dir string) ([]string, error) {
	const createTracking = `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   TEXT        PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
		)`
	if _, err := db.ExecContext(ctx, createTracking); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	files, err := migrationFiles(dir)
	if err != nil {
		return nil, err
	}

	var applied []string
	for _, file := range files {
		ran, err := applyMigration(ctx, db, dir, file)
		if err != nil {
			return applied, fmt.Errorf("apply migration %s: %w", file, err)
		}
		if ran {
			applied = append(applied, file)
		}
	}
	return applied, nil
}

func migrationFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read migrations directory %s: %w", dir, err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			files = append(files, entry.Name())
		}
	}
	sort.Strings(files)
	return files, nil
}

func applyMigration(ctx context.Context, db *sql.DB, dir, file string) (bool, error) {
	statements, err := os.ReadFile(filepath.Join(dir, file))
	if err != nil {
		return false, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1) ON CONFLICT DO NOTHING`, file)
	if err != nil {
		return false, err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if affected == 0 {
		return false, tx.Commit()
	}

	if _, err := tx.ExecContext(ctx, string(statements)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

type Health struct{ db *sql.DB }

func NewHealth(db *sql.DB) *Health { return &Health{db: db} }

func (h *Health) Ping(ctx context.Context) error { return h.db.PingContext(ctx) }

