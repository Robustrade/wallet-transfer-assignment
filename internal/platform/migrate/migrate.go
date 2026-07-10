// Package migrate runs pending golang-migrate migrations at startup,
// before the server binds. It has no domain knowledge — see
// docs/DESIGN.md §8.
package migrate

import (
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	// Registers the "postgres" database driver used by migrate.New below.
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	// Registers the "file" source driver used by migrate.New below.
	_ "github.com/golang-migrate/migrate/v4/source/file"
)

// Run applies all pending migrations under migrationsPath to dsn.
// A no-change result is not an error.
func Run(dsn, migrationsPath string) error {
	m, err := migrate.New("file://"+migrationsPath, dsn)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer func() {
		_, _ = m.Close()
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("run migrations: %w", err)
	}
	return nil
}
