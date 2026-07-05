//go:build integration

// Package testutil provides shared helpers for integration tests that run
// against a real Postgres instance (see the Makefile's test-integration
// target: docker compose up -d postgres && make migrate-up first).
package testutil

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/config"
)

// NewPool connects to Postgres using the same DB_* env vars cmd/server
// reads, verifies connectivity, and closes the pool when the test ends.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg := config.Load()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DSN())
	if err != nil {
		t.Fatalf("connect to test db: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping test db (is `docker compose up -d postgres && make migrate-up` running?): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TruncateAll clears every table so a test starts from a clean slate. Call
// it once at the start of a test, before spawning any goroutines against
// the same pool.
func TruncateAll(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`TRUNCATE TABLE ledger_entries, idempotency_records, transfers, wallets RESTART IDENTITY CASCADE`)
	if err != nil {
		t.Fatalf("truncate tables: %v", err)
	}
}

// SeedWallet inserts a wallet with the given starting balance.
func SeedWallet(t *testing.T, pool *pgxpool.Pool, id string, balance int64) {
	t.Helper()
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance)
	if err != nil {
		t.Fatalf("seed wallet %s: %v", id, err)
	}
}
