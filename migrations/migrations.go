package migrations

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// FS embeds all SQL migration files in this directory.
//
//go:embed *.sql
var FS embed.FS

// Migrate executes all embedded migration scripts in lexical order with atomic version tracking.
func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	// Acquire a dedicated migration advisory lock to serialize concurrent application startups
	lockConn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("failed to acquire connection for migration lock: %w", err)
	}
	defer lockConn.Release()

	const migrationAdvisoryLockID = 888999
	// Register the unlock defer immediately after acquiring the connection, before attempting pg_advisory_lock.
	// If pg_advisory_lock fails after PostgreSQL acquires the session lock (e.g. context cancellation or
	// network timeout), this defer ensures the lock is freed with context.Background() before lockConn.Release()
	// returns the connection to the pool.
	defer func() {
		_, _ = lockConn.Exec(context.Background(), "SELECT pg_advisory_unlock($1);", migrationAdvisoryLockID)
	}()

	if _, err := lockConn.Exec(ctx, "SELECT pg_advisory_lock($1);", migrationAdvisoryLockID); err != nil {
		return fmt.Errorf("failed to acquire migration advisory lock: %w", err)
	}

	// Create schema_migrations table if not exists to track applied versions
	createTableQuery := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`
	// Run schema table creation and queries directly on lockConn to avoid acquiring additional pool connections
	// (ensuring migrations complete safely even on pools with MaxConns == 1).
	if _, err := lockConn.Exec(ctx, createTableQuery); err != nil {
		return fmt.Errorf("failed to initialize schema_migrations table: %w", err)
	}

	entries, err := fs.ReadDir(FS, ".")
	if err != nil {
		return fmt.Errorf("failed to read migrations directory: %w", err)
	}

	var sqlFiles []string
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".sql" {
			sqlFiles = append(sqlFiles, entry.Name())
		}
	}
	sort.Strings(sqlFiles)

	for _, file := range sqlFiles {
		// Check if this migration has already been applied
		var appliedVersion string
		err := lockConn.QueryRow(ctx, "SELECT version FROM schema_migrations WHERE version = $1", file).Scan(&appliedVersion)
		if err == nil {
			// Already applied; skip safely
			continue
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("failed to check applied status for migration %s: %w", file, err)
		}

		content, err := FS.ReadFile(file)
		if err != nil {
			return fmt.Errorf("failed to read migration file %s: %w", file, err)
		}

		if err := applyMigration(ctx, lockConn, file, string(content)); err != nil {
			return err
		}
	}

	return nil
}

func applyMigration(ctx context.Context, conn *pgxpool.Conn, file string, content string) error {
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction for migration %s: %w", file, err)
	}

	committed := false
	defer func() {
		if !committed {
			rollbackCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = tx.Rollback(rollbackCtx)
		}
	}()

	if _, err := tx.Exec(ctx, content); err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", file, err)
	}

	if _, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version) VALUES ($1)", file); err != nil {
		return fmt.Errorf("failed to record applied migration %s: %w", file, err)
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit migration %s: %w", file, err)
	}
	committed = true
	return nil
}
