// Package repository selects and opens the configured database.
package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"time"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/config"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository/sqlite"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/repository/sqlrepo"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// Options selects and configures the database.
type Options struct {
	Driver      string // config.DriverSQLite or config.DriverPostgres
	DSN         string
	LockTimeout time.Duration
	// SeedDemoData applies the *_seed.sql migrations (demo wallets).
	SeedDemoData bool
}

// Store is an open, migrated database with everything the services need.
type Store struct {
	// DB serves reads, pings and migrations. On Postgres it also serves
	// writes. On SQLite, writes use a separate single-connection pool (see
	// sqlite.OpenWriter).
	DB        *sql.DB
	Dialect   sqlrepo.Dialect
	TxManager *sqlrepo.TxManager
	// Repos are bound to the pool, not to a transaction. Read APIs use them;
	// writes must go through TxManager.
	Repos service.Repos

	writeDB *sql.DB // == DB except on SQLite
}

// Open connects to the configured database, verifies the connection and
// applies pending migrations. Any other driver name is an error, so a typo in
// DB_DRIVER fails at startup instead of at the first request.
func Open(ctx context.Context, opts Options) (*Store, error) {
	var (
		db, writeDB *sql.DB
		dialect     sqlrepo.Dialect
		migrations  fs.FS
		err         error
	)
	switch opts.Driver {
	case config.DriverSQLite:
		db, err = sqlite.Open(opts.DSN, opts.LockTimeout)
		if err == nil {
			writeDB, err = sqlite.OpenWriter(opts.DSN, opts.LockTimeout)
		}
		dialect, migrations = sqlite.Dialect{}, sqlite.Migrations
	case config.DriverPostgres:
		db, err = postgres.Open(opts.DSN)
		writeDB = db
		dialect, migrations = postgres.NewDialect(opts.LockTimeout), postgres.Migrations
	default:
		return nil, fmt.Errorf("unsupported database driver %q (want %q or %q)",
			opts.Driver, config.DriverSQLite, config.DriverPostgres)
	}
	store := &Store{DB: db, writeDB: writeDB, Dialect: dialect}
	if err != nil {
		_ = store.Close()
		return nil, err
	}

	if err := db.PingContext(ctx); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("connect to %s: %w", opts.Driver, err)
	}
	if err := sqlrepo.Migrate(ctx, db, dialect, migrations, opts.SeedDemoData); err != nil {
		_ = store.Close()
		return nil, fmt.Errorf("migrate %s: %w", opts.Driver, err)
	}

	store.TxManager = sqlrepo.NewTxManager(writeDB, dialect)
	store.Repos = sqlrepo.NewRepos(db, dialect)
	return store, nil
}

// Close closes the connection pool(s). It is safe on a partially opened Store.
func (s *Store) Close() error {
	var errs []error
	if s.writeDB != nil && s.writeDB != s.DB {
		errs = append(errs, s.writeDB.Close())
	}
	if s.DB != nil {
		errs = append(errs, s.DB.Close())
	}
	return errors.Join(errs...)
}
