// Package testutil provides shared test infrastructure for integration tests.
// It requires a live PostgreSQL instance pointed to by the TEST_DATABASE_URL
// environment variable. Tests skip automatically when the variable is unset.
package testutil

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/jackc/pgx/v5/pgxpool"
)

// NewTestPool opens a pool to the test database, applies any pending
// migrations, and registers a cleanup that truncates all data tables after
// each test so tests are fully isolated from each other.
//
// If TEST_DATABASE_URL is not set the test is skipped. This allows the
// domain unit tests (which need no database) to run in environments where
// Postgres is not available.
func NewTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("skipping integration test: TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to test database: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Skipf("skipping integration test: test database unreachable: %v", err)
	}

	runMigrations(t, dsn)

	t.Cleanup(func() {
		truncateAll(t, pool)
		pool.Close()
	})

	return pool
}

func runMigrations(t *testing.T, dsn string) {
	t.Helper()
	m, err := migrate.New("file://../../db/migrations", dsn)
	if err != nil {
		t.Fatalf("migrate.New: %v", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		t.Fatalf("migrate up: %v", err)
	}
}

func truncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	// Truncate in dependency order (child tables first).
	tables := []string{"ledger_entries", "transfers", "wallets"}
	for _, tbl := range tables {
		if _, err := pool.Exec(context.Background(),
			fmt.Sprintf("TRUNCATE TABLE %s RESTART IDENTITY CASCADE", tbl),
		); err != nil {
			t.Errorf("truncate %s: %v", tbl, err)
		}
	}
}

// SeedWallet inserts a wallet with the given id and balance for test setup.
func SeedWallet(t *testing.T, pool *pgxpool.Pool, id string, balance int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance,
	)
	if err != nil {
		t.Fatalf("SeedWallet %s: %v", id, err)
	}
}

// WalletBalance reads the current balance for a wallet, failing the test if
// the wallet does not exist.
func WalletBalance(t *testing.T, pool *pgxpool.Pool, id string) int64 {
	t.Helper()
	var balance int64
	err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, id,
	).Scan(&balance)
	if err != nil {
		t.Fatalf("WalletBalance %s: %v", id, err)
	}
	return balance
}

// LedgerEntryCount returns the number of ledger entries for a given transfer ID.
func LedgerEntryCount(t *testing.T, pool *pgxpool.Pool, transferID string) int {
	t.Helper()
	var count int
	err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, transferID,
	).Scan(&count)
	if err != nil {
		t.Fatalf("LedgerEntryCount %s: %v", transferID, err)
	}
	return count
}
