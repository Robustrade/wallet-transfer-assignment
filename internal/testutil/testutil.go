// Package testutil holds fixture helpers shared by the integration test
// suites in internal/repository/postgres and internal/service — each
// test still creates its own fixtures via these helpers (see CLAUDE.md's
// "every test creates its own wallet/transfer fixtures" rule); this only
// avoids duplicating the helpers themselves across packages.
package testutil

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
)

// PostgresDSN points at the postgres brought up by `docker-compose up -d
// postgres redis` (see `make test-integration`), overridable for CI.
func PostgresDSN() string {
	if dsn := os.Getenv("TEST_POSTGRES_DSN"); dsn != "" {
		return dsn
	}
	return "postgres://wallet:wallet@localhost:5432/wallet?sslmode=disable"
}

// NewTestWallet inserts a fresh wallet with a unique ID and the given
// balance directly via SQL. Every integration test creates its own
// wallets — never reuses another test's — so concurrent test runs can
// never corrupt each other's expected balances.
func NewTestWallet(t *testing.T, pool *pgxpool.Pool, balance string) domain.Wallet {
	t.Helper()

	w := domain.Wallet{
		ID:      uuid.NewString(),
		Balance: decimal.RequireFromString(balance),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := pool.Exec(ctx, `
		INSERT INTO wallets (id, balance, created_at, updated_at)
		VALUES ($1, $2, now(), now())
	`, w.ID, w.Balance)
	if err != nil {
		t.Fatalf("insert test wallet: %v", err)
	}
	return w
}
