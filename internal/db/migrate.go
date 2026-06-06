package db

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres" // postgres driver
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

// Migrate applies all pending up migrations from the provided fs.FS.
// The path argument is the directory within fsys that contains the *.sql files.
// Using fs.FS avoids platform-specific file:// URL issues and supports both
// embedded and OS-based file systems.
func Migrate(databaseURL string, fsys fs.FS, path string) error {
	src, err := iofs.New(fsys, path)
	if err != nil {
		return fmt.Errorf("db: create iofs source: %w", err)
	}

	m, err := migrate.NewWithSourceInstance("iofs", src, databaseURL)
	if err != nil {
		return fmt.Errorf("db: create migrator: %w", err)
	}

	defer func() {
		srcErr, dbErr := m.Close()
		if srcErr != nil {
			slog.Error("migration source close error", "layer", "db", "error", srcErr)
		}

		if dbErr != nil {
			slog.Error("migration db close error", "layer", "db", "error", dbErr)
		}
	}()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("db: apply migrations: %w", err)
	}

	slog.Info("migrations applied", "layer", "db")

	return nil
}
