package repository_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/repository"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// dsn returns the test database connection string.
func dsn() string {
	if v := os.Getenv("DATABASE_URL"); v != "" {
		return v
	}
	return "postgres://wallet:wallet@localhost:5433/wallet_db?sslmode=disable"
}

// newTestStore connects to Postgres or skips the test if unreachable.
func newTestStore(t *testing.T) *repository.PostgresStore {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	store, err := repository.NewPostgresStore(ctx, dsn())
	if err != nil {
		t.Skipf("skipping integration test, db unreachable: %v", err)
	}
	return store
}

// seed inserts a wallet with the given balance using a raw pool connection.
func seed(t *testing.T, ctx context.Context, id string, balance int64) {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn())
	if err != nil {
		t.Fatalf("seed pool: %v", err)
	}
	defer pool.Close()

	_, err = pool.Exec(ctx, `INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance)
	if err != nil {
		t.Fatalf("seed wallet %s: %v", id, err)
	}
}

func walletBalance(t *testing.T, ctx context.Context, id string) int64 {
	t.Helper()
	pool, err := pgxpool.New(ctx, dsn())
	if err != nil {
		t.Fatalf("balance pool: %v", err)
	}
	defer pool.Close()

	var b int64
	if err := pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, id).Scan(&b); err != nil {
		t.Fatalf("read balance %s: %v", id, err)
	}
	return b
}

// TestConcurrentDebits_NoDoubleSpend fires many concurrent transfers from a
// wallet that can only fund one. Exactly one must succeed; the rest must fail
// with insufficient balance, and the final balance must never go negative.
func TestConcurrentDebits_NoDoubleSpend(t *testing.T) {
	store := newTestStore(t)
	defer store.Close()

	ctx := context.Background()

	// Unique wallet ids per run to avoid collisions across repeated runs.
	run := uuid.NewString()[:8]
	src := fmt.Sprintf("src_%s", run)
	dst := fmt.Sprintf("dst_%s", run)

	// Source can fund exactly one transfer of 100.
	seed(t, ctx, src, 100)
	seed(t, ctx, dst, 0)

	svc := service.NewTransferService(store)

	const n = 10
	var wg sync.WaitGroup
	results := make([]error, n)

	wg.Add(n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			_, _, err := svc.CreateTransfer(ctx, service.TransferRequest{
				IdempotencyKey: uuid.NewString(), // distinct keys: these are NOT retries
				FromWalletID:   src,
				ToWalletID:     dst,
				Amount:         100,
			})
			results[idx] = err
		}(i)
	}
	wg.Wait()

	var success, insufficient, other int
	for _, err := range results {
		switch {
		case err == nil:
			success++
		case errors.Is(err, service.ErrInsufficientBalance):
			insufficient++
		default:
			other++
		}
	}

	if success != 1 {
		t.Errorf("successful transfers = %d, want exactly 1", success)
	}
	if insufficient != n-1 {
		t.Errorf("insufficient-balance failures = %d, want %d", insufficient, n-1)
	}
	if other != 0 {
		t.Errorf("unexpected errors = %d, want 0", other)
	}

	// Final balances: src fully drained, dst received exactly one transfer.
	if got := walletBalance(t, ctx, src); got != 0 {
		t.Errorf("source balance = %d, want 0", got)
	}
	if got := walletBalance(t, ctx, dst); got != 100 {
		t.Errorf("dest balance = %d, want 100", got)
	}
}