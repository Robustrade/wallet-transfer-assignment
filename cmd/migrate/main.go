package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/devkarrahul2610/wallet-transfer-assignment/internal/config"
	"github.com/devkarrahul2610/wallet-transfer-assignment/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		slog.Error("migration failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	db, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer db.Close()

	if _, err := db.ExecContext(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	files, err := filepath.Glob(filepath.Join("migrations", "*.up.sql"))
	if err != nil {
		return fmt.Errorf("find migrations: %w", err)
	}
	sort.Strings(files)
	if len(files) == 0 {
		return errors.New("no up migrations found in ./migrations")
	}

	for _, filename := range files {
		version := filepath.Base(filename)
		applied, err := applyMigration(ctx, db, version, filename)
		if err != nil {
			return err
		}
		if applied {
			slog.Info("migration applied", "version", version)
		}
	}

	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, version, filename string) (bool, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin migration %s: %w", version, err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(761923441)`); err != nil {
		return false, fmt.Errorf("lock migration runner: %w", err)
	}

	var exists bool
	if err := tx.QueryRowContext(ctx,
		`SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, version,
	).Scan(&exists); err != nil {
		return false, fmt.Errorf("check migration %s: %w", version, err)
	}
	if exists {
		return false, nil
	}

	sqlBytes, err := os.ReadFile(filename)
	if err != nil {
		return false, fmt.Errorf("read migration %s: %w", version, err)
	}
	if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
		return false, fmt.Errorf("execute migration %s: %w", version, err)
	}

	if _, err := tx.ExecContext(ctx,
		`INSERT INTO schema_migrations (version) VALUES ($1)`, version,
	); err != nil {
		return false, fmt.Errorf("record migration %s: %w", version, err)
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit migration %s: %w", version, err)
	}
	return true, nil
}
