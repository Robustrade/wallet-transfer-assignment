package migrations

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
	"strings"
)

//go:embed sqlite/*.sql postgres/*.sql
var files embed.FS

// Apply runs pending .sql migration files for the given driver in lexical order.
// Each file is recorded in schema_migrations and is not executed again on later startups.
// driver must be "sqlite" or "postgres".
func Apply(db *sql.DB, driver string) error {
	switch driver {
	case "sqlite", "postgres":
	default:
		return fmt.Errorf("unsupported migration driver: %s", driver)
	}

	if err := ensureSchemaMigrationsTable(db, driver); err != nil {
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	pattern := path.Join(driver, "*.sql")
	migrationFiles, err := fs.Glob(files, pattern)
	if err != nil {
		return fmt.Errorf("list migrations: %w", err)
	}
	if len(migrationFiles) == 0 {
		return fmt.Errorf("no migrations found for driver %s", driver)
	}

	sort.Strings(migrationFiles)

	for _, file := range migrationFiles {
		version := path.Base(file)

		applied, err := isApplied(db, driver, version)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if applied {
			continue
		}

		content, err := files.ReadFile(file)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", file, err)
		}

		sqlText := strings.TrimSpace(string(content))
		if sqlText == "" {
			if err := recordMigration(db, driver, version); err != nil {
				return fmt.Errorf("record empty migration %s: %w", version, err)
			}
			continue
		}

		if err := runMigration(db, driver, version, sqlText); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
	}

	return nil
}

func ensureSchemaMigrationsTable(db *sql.DB, driver string) error {
	var ddl string
	switch driver {
	case "sqlite":
		ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		)`
	case "postgres":
		ddl = `CREATE TABLE IF NOT EXISTS schema_migrations (
			version    TEXT PRIMARY KEY,
			applied_at TIMESTAMP NOT NULL DEFAULT NOW()
		)`
	}
	_, err := db.Exec(ddl)
	return err
}

func isApplied(db *sql.DB, driver, version string) (bool, error) {
	var count int
	var err error
	switch driver {
	case "postgres":
		err = db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE version = $1`, version,
		).Scan(&count)
	default:
		err = db.QueryRow(
			`SELECT COUNT(*) FROM schema_migrations WHERE version = ?`, version,
		).Scan(&count)
	}
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func runMigration(db *sql.DB, driver, version, sqlText string) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(sqlText); err != nil {
		return fmt.Errorf("exec sql: %w", err)
	}
	if err := recordMigrationTx(tx, driver, version); err != nil {
		return err
	}
	return tx.Commit()
}

func recordMigration(db *sql.DB, driver, version string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if err := recordMigrationTx(tx, driver, version); err != nil {
		return err
	}
	return tx.Commit()
}

func recordMigrationTx(tx *sql.Tx, driver, version string) error {
	var err error
	switch driver {
	case "postgres":
		_, err = tx.Exec(`INSERT INTO schema_migrations (version) VALUES ($1)`, version)
	default:
		_, err = tx.Exec(`INSERT INTO schema_migrations (version) VALUES (?)`, version)
	}
	if err != nil {
		return fmt.Errorf("record migration version: %w", err)
	}
	return nil
}
