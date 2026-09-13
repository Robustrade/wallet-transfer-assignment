package datasource

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"wallet-transfer-assigment/utils"

	"go.uber.org/zap"
)

func ensureMigrationTable(con *sql.DB) error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version INTEGER PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);
	`

	_, err := con.Exec(query)
	return err
}

func runMigrations(con *sql.DB, log utils.Logger) error {
	log.Info("Migration process started")

	// 1. Make sure migration tracking table exists.
	log.Info("Ensuring migration tracking table exists")

	if err := ensureMigrationTable(con); err != nil {
		log.Error(
			"Failed to create migration tracking table",
			zap.Error(err),
		)
		return fmt.Errorf("create migration table: %w", err)
	}

	log.Info("Migration tracking table is ready")

	// 2. Find all migration files.
	files, err := filepath.Glob("db/migrations/*.sql")
	if err != nil {
		log.Error(
			"Failed to find migration files",
			zap.Error(err),
		)
		return fmt.Errorf("find migration files: %w", err)
	}

	log.Info(
		"Migration files discovered",
		zap.Int("count", len(files)),
	)

	// 3. Sort migrations: 001, 002, 003...
	sort.Strings(files)

	for _, file := range files {
		// Example:
		// db/migrations/001_init.sql
		// version = 1

		version, err := migrationVersion(file)
		if err != nil {
			log.Error(
				"Invalid migration file",
				zap.String("file", file),
				zap.Error(err),
			)
			return err
		}

		log.Info(
			"Checking migration",
			zap.Int("version", version),
			zap.String("file", file),
		)

		// 4. Check whether this migration was already applied.
		applied, err := isMigrationApplied(con, version)
		if err != nil {
			log.Error(
				"Failed to check migration status",
				zap.Int("version", version),
				zap.Error(err),
			)

			return fmt.Errorf(
				"check migration %d: %w",
				version,
				err,
			)
		}

		if applied {
			log.Info(
				"Migration already applied, skipping",
				zap.Int("version", version),
				zap.String("file", file),
			)

			continue
		}

		log.Info(
			"Migration pending",
			zap.Int("version", version),
			zap.String("file", file),
		)

		// 5. Read migration SQL.
		sqlBytes, err := os.ReadFile(file)
		if err != nil {
			log.Error(
				"Failed to read migration file",
				zap.Int("version", version),
				zap.String("file", file),
				zap.Error(err),
			)

			return fmt.Errorf(
				"read migration %s: %w",
				file,
				err,
			)
		}

		// 6. Run migration + record version atomically.
		log.Info(
			"Applying migration",
			zap.Int("version", version),
			zap.String("file", file),
		)

		if err := applyMigration(
			con,
			version,
			string(sqlBytes),
		); err != nil {
			log.Error(
				"Migration failed",
				zap.Int("version", version),
				zap.String("file", file),
				zap.Error(err),
			)

			return fmt.Errorf(
				"apply migration %d: %w",
				version,
				err,
			)
		}

		log.Info(
			"Migration applied successfully",
			zap.Int("version", version),
			zap.String("file", file),
		)
	}

	log.Info("Migration process completed successfully")

	return nil
}

func migrationVersion(file string) (int, error) {
	filename := filepath.Base(file)

	// Example:
	// 001_init.sql

	parts := strings.SplitN(filename, "_", 2)

	if len(parts) != 2 {
		return 0, fmt.Errorf(
			"invalid migration filename: %s",
			filename,
		)
	}

	version, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf(
			"invalid migration version in %s: %w",
			filename,
			err,
		)
	}

	return version, nil
}

func isMigrationApplied(
	con *sql.DB,
	version int,
) (bool, error) {

	var exists bool

	query := `
		SELECT EXISTS (
			SELECT 1
			FROM schema_migrations
			WHERE version = ?
	);
	`

	err := con.QueryRow(query, version).Scan(&exists)
	if err != nil {
		return false, err
	}

	return exists, nil
}

func applyMigration(
	con *sql.DB,
	version int,
	migrationSQL string,
) error {

	tx, err := con.Begin()
	if err != nil {
		return err
	}

	defer func() {
		_ = tx.Rollback()
	}()

	// Execute migration.
	if _, err := tx.Exec(migrationSQL); err != nil {
		return err
	}

	// Only record the migration AFTER successful execution.
	query := `
		INSERT INTO schema_migrations (version)
		VALUES (?);
	`

	if _, err := tx.Exec(query, version); err != nil {
		return err
	}

	// Commit migration + version record together.
	if err := tx.Commit(); err != nil {
		return err
	}

	return nil
}
