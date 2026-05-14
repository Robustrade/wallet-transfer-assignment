package service_test

// These are integration tests that require a real PostgreSQL instance.
// Set the DATABASE_URL environment variable before running:
//
//	DATABASE_URL=postgres://postgres:postgres@localhost:5432/wallet_transfer_test go test ./internal/service/...

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"wallet-transfer/internal/db"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
)

// newTestService connects to the test database and returns a wired Service and
// the underlying pool (for tests that need to query the DB directly).
// The pool is closed automatically when the test ends.
func newTestService(t *testing.T) (*service.Service, *pgxpool.Pool) {
	t.Helper()

	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set — skipping integration tests")
	}

	ctx := context.Background()
	pool, err := db.NewPool(ctx, dsn)
	require.NoError(t, err, "failed to connect to test database")

	t.Cleanup(func() { pool.Close() })

	svc := service.New(
		pool,
		repository.NewWalletRepository(),
		repository.NewTransferRepository(),
		repository.NewLedgerRepository(),
	)
	return svc, pool
}

// seedWallet creates a wallet with the given balance for use in tests.
func seedWallet(t *testing.T, svc *service.Service, id string, balance int64) *domain.Wallet {
	t.Helper()
	w, err := svc.CreateWallet(context.Background(), service.CreateWalletRequest{
		ID:             id,
		InitialBalance: balance,
	})
	require.NoError(t, err)
	return w
}

// unique returns a test-scoped unique string to avoid ID collisions between test runs.
func unique(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}
