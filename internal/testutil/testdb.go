// Package testutil sets up a real Postgres database for integration tests.
package testutil

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/db"
)

const defaultTestURL = "postgres://postgres:postgres@localhost:5432/wallet_test?sslmode=disable"

// DB connects to TEST_DATABASE_URL, applies migrations and empties every table.
// Integration tests are skipped with `go test -short`.
func DB(t *testing.T) *sql.DB {
	t.Helper()
	if testing.Short() {
		t.Skip("integration test: needs Postgres")
	}
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		url = defaultTestURL
	}

	ctx := context.Background()
	conn, err := db.Open(ctx, url)
	if err != nil {
		t.Fatalf("connect to test database (%s): %v", url, err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if err := db.Migrate(ctx, conn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	MustExec(t, conn,
		`TRUNCATE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE`)
	return conn
}

// MustExec runs a statement and fails the test on error.
func MustExec(t *testing.T, conn *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := conn.Exec(query, args...); err != nil {
		t.Fatalf("exec %q: %v", query, err)
	}
}

// SeedWallet inserts a wallet with an opening balance.
func SeedWallet(t *testing.T, conn *sql.DB, id string, balance int64) {
	t.Helper()
	MustExec(t, conn, `INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance)
}

// Balance reads a wallet balance.
func Balance(t *testing.T, conn *sql.DB, id string) int64 {
	t.Helper()
	var b int64
	if err := conn.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, id).Scan(&b); err != nil {
		t.Fatalf("read balance of %s: %v", id, err)
	}
	return b
}

// Count returns the number of rows in a table.
func Count(t *testing.T, conn *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := conn.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}
