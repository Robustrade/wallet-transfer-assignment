//go:build integration

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/repository"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/testutil"
)

func TestWalletRepository_GetWalletByID(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 500)

	repo := repository.NewWalletRepository()

	t.Run("found", func(t *testing.T) {
		wallet, err := repo.GetWalletByID(context.Background(), pool, "wallet_1")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if wallet.Balance != 500 {
			t.Errorf("expected balance 500, got %d", wallet.Balance)
		}
	})

	t.Run("not found", func(t *testing.T) {
		_, err := repo.GetWalletByID(context.Background(), pool, "does-not-exist")
		if !errors.Is(err, domain.ErrWalletNotFound) {
			t.Errorf("expected ErrWalletNotFound, got %v", err)
		}
	})
}

// TestWalletRepository_LockForUpdate_BlocksConcurrentTx confirms FOR UPDATE
// actually takes a row lock: a second transaction attempting to lock the
// same wallet must block until the first commits/rolls back, not just
// return the row's snapshot.
func TestWalletRepository_LockForUpdate_BlocksConcurrentTx(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)

	repo := repository.NewWalletRepository()
	ctx := context.Background()

	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx)

	if _, err := repo.LockForUpdate(ctx, tx1, []string{"wallet_1"}); err != nil {
		t.Fatalf("tx1 lock: %v", err)
	}

	unblocked := make(chan struct{})
	go func() {
		tx2, err := pool.Begin(ctx)
		if err != nil {
			t.Errorf("begin tx2: %v", err)
			return
		}
		defer tx2.Rollback(ctx)
		if _, err := repo.LockForUpdate(ctx, tx2, []string{"wallet_1"}); err != nil {
			t.Errorf("tx2 lock: %v", err)
			return
		}
		close(unblocked)
	}()

	select {
	case <-unblocked:
		t.Fatal("expected tx2's LockForUpdate to block while tx1 holds the row lock")
	case <-time.After(300 * time.Millisecond):
	}

	if err := tx1.Rollback(ctx); err != nil {
		t.Fatalf("rollback tx1: %v", err)
	}

	select {
	case <-unblocked:
	case <-time.After(2 * time.Second):
		t.Fatal("tx2 did not acquire the lock after tx1 released it")
	}
}

func TestWalletRepository_UpdateBalance(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)

	repo := repository.NewWalletRepository()
	if err := repo.UpdateBalance(context.Background(), pool, "wallet_1", 250); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	wallet, err := repo.GetWalletByID(context.Background(), pool, "wallet_1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wallet.Balance != 250 {
		t.Errorf("expected balance 250, got %d", wallet.Balance)
	}
}

// TestWalletRepository_UpdateBalance_RejectsNegativeBalance proves the
// wallets.balance >= 0 CHECK constraint is enforced by Postgres itself, not
// just by the service layer's HasSufficientFunds guard.
func TestWalletRepository_UpdateBalance_RejectsNegativeBalance(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 50)

	repo := repository.NewWalletRepository()
	err := repo.UpdateBalance(context.Background(), pool, "wallet_1", -1)
	if err == nil {
		t.Fatal("expected the balance >= 0 CHECK constraint to reject a negative balance, got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("expected a check_violation (23514), got %v", err)
	}
}
