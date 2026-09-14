// Package dbtest provides the fixtures shared by the integration tests: a
// migrated database, and wallets to move money between.
//
// Integration tests run against a real PostgreSQL rather than a mock. A mocked
// query passes happily while the SQL underneath it is wrong, and the SQL is
// where the locking, the constraints, and the idempotency claim actually live.
package dbtest

import (
	"context"
	"os"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/db"
	"github.com/sagarsuperuser/wallet-transfer-assignment/migrations"
)

// The schema is applied once per test binary. migrateErr is package level, not
// local to Pool, so every test reports the real migration failure rather than
// only the one that happened to run first — the rest would otherwise see a nil
// error and fail later on missing tables, which points at the wrong layer.
// The schema is applied once per test binary. migrateErr is package level, not
// local to Pool, so every test reports the real migration failure rather than
// only the one that happened to run first — the rest would otherwise see a nil
// error and fail later on missing tables, which points at the wrong layer.
var (
	migrateOnce sync.Once
	migrateErr  error
)

// Pool connects to the database named by TEST_DATABASE_URL and ensures the
// schema is applied.
//
// A missing database is a convenience locally and a failure in CI. On a
// developer machine `go test ./...` should say "skipped, no database" rather
// than produce a dozen connection errors; in CI a green build has to mean the
// integration tests actually ran, because a skipped test and a passing test
// otherwise look identical and coverage can silently fall to zero.
//
// The check lives here rather than only in the workflow so that it holds however
// CI is wired: a later edit to ci.yml cannot quietly disable these tests without
// the suite objecting.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		if os.Getenv("CI") != "" {
			t.Fatal("TEST_DATABASE_URL is not set in CI; integration tests must not be skipped")
		}
		t.Skip("TEST_DATABASE_URL is not set; skipping integration test")
	}

	pool, err := db.Open(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(pool.Close)

	migrateOnce.Do(func() {
		migrateErr = db.Migrate(context.Background(), pool, migrations.FS)
	})
	if migrateErr != nil {
		t.Fatalf("migrate: %v", migrateErr)
	}

	return pool
}

// Wallet inserts a wallet holding balance and returns its id.
//
// The id is unique per call so tests never contend for the same rows, which
// keeps them independent and lets them run in parallel.
func Wallet(t *testing.T, pool *pgxpool.Pool, balance int64) string {
	t.Helper()

	id := "wallet_" + uuid.NewString()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance); err != nil {
		t.Fatalf("seed wallet: %v", err)
	}

	return id
}

// Balance reads a wallet's stored balance with a plain query, independent of
// the code under test, so an assertion cannot be fooled by the same bug twice.
func Balance(t *testing.T, pool *pgxpool.Pool, walletID string) int64 {
	t.Helper()

	var balance int64
	if err := pool.QueryRow(context.Background(),
		`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&balance); err != nil {
		t.Fatalf("read balance of %s: %v", walletID, err)
	}

	return balance
}

// LedgerNet returns a wallet's credits minus its debits, read with a plain
// query. A wallet's stored balance must always equal the balance it was seeded
// with plus this number.
func LedgerNet(t *testing.T, pool *pgxpool.Pool, walletID string) int64 {
	t.Helper()

	var net int64
	if err := pool.QueryRow(context.Background(), `
		SELECT COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE -amount END), 0)
		FROM ledger_entries
		WHERE wallet_id = $1`, walletID).Scan(&net); err != nil {
		t.Fatalf("sum ledger entries for %s: %v", walletID, err)
	}

	return net
}

// CountTransfers returns how many transfers exist for an idempotency key. It
// should never exceed one; the unique constraint makes more impossible, and
// asserting it proves the constraint is doing the work.
func CountTransfers(t *testing.T, pool *pgxpool.Pool, idempotencyKey string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM transfers WHERE idempotency_key = $1`,
		idempotencyKey).Scan(&count); err != nil {
		t.Fatalf("count transfers for key %s: %v", idempotencyKey, err)
	}

	return count
}

// CountLedgerEntries returns how many ledger entries a transfer produced.
func CountLedgerEntries(t *testing.T, pool *pgxpool.Pool, transferID string) int {
	t.Helper()

	var count int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`,
		transferID).Scan(&count); err != nil {
		t.Fatalf("count ledger entries for transfer %s: %v", transferID, err)
	}

	return count
}
