// Package testutils holds shared helpers for database-backed integration tests.
package testutils

import (
	"context"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"wallet-transfer/internal/repository/postgres"
)

// NewPool connects to TEST_DATABASE_URL (schema pre-applied) or skips the test.
func NewPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), url)
	require.NoError(t, err)
	require.NoError(t, pool.Ping(context.Background()))
	t.Cleanup(pool.Close)
	return pool
}

// NewStore returns a pool and a Postgres store.
func NewStore(t *testing.T) (*pgxpool.Pool, *postgres.Store) {
	pool := NewPool(t)
	return pool, postgres.New(pool)
}

// Money parses an exact decimal.
func Money(v string) decimal.Decimal { return decimal.RequireFromString(v) }

// CreateWallet inserts a wallet and returns its id.
func CreateWallet(t *testing.T, pool *pgxpool.Pool, balance string, active bool) uuid.UUID {
	t.Helper()
	var id uuid.UUID
	require.NoError(t, pool.QueryRow(context.Background(),
		`INSERT INTO wallet (balance, is_active) VALUES ($1, $2) RETURNING id`,
		Money(balance), active).Scan(&id))
	return id
}

// Balance reads a wallet's current balance.
func Balance(t *testing.T, pool *pgxpool.Pool, id uuid.UUID) decimal.Decimal {
	t.Helper()
	var b decimal.Decimal
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT balance FROM wallet WHERE id = $1`, id).Scan(&b))
	return b
}

// LedgerCount counts ledger legs for a transfer.
func LedgerCount(t *testing.T, pool *pgxpool.Pool, transferID uuid.UUID) int {
	t.Helper()
	var n int
	require.NoError(t, pool.QueryRow(context.Background(),
		`SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, transferID).Scan(&n))
	return n
}
