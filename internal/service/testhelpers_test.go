package service_test

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/repository/postgres"
)

// testPool connects to a real Postgres instance for integration testing.
// These tests exercise row locking and transaction semantics that a mock
// cannot faithfully reproduce, so a live database is required. If one isn't
// reachable, the test is skipped rather than failed, so `go test ./...`
// still succeeds in environments without Postgres configured.
func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		dsn = os.Getenv("DATABASE_URL")
	}
	if dsn == "" {
		dsn = "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
	}

	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, dsn)
	if err != nil {
		t.Skipf("skipping integration test: cannot connect to postgres at %q: %v", dsn, err)
	}
	if err := postgres.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedWallet inserts a wallet with a fresh, collision-free id so each test
// gets isolated state without needing to truncate shared tables.
func seedWallet(t *testing.T, pool *pgxpool.Pool, balance int64) string {
	t.Helper()
	id := "test_" + uuid.NewString()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance)
	if err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	return id
}
