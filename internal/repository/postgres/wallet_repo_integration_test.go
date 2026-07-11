//go:build integration

package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestWalletRepo_GetForUpdate_ReturnsLockedWallets(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "100.00")
	b := newTestWallet(t, "50.00")

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	repo := NewWalletRepo()
	ids := []string{a.ID, b.ID}
	if ids[0] > ids[1] {
		ids[0], ids[1] = ids[1], ids[0]
	}

	wallets, err := repo.GetForUpdate(ctx, tx, ids...)
	if err != nil {
		t.Fatalf("GetForUpdate: %v", err)
	}
	if len(wallets) != 2 {
		t.Fatalf("expected 2 wallets, got %d", len(wallets))
	}
}

func TestWalletRepo_GetForUpdate_MissingWalletReturnsFewer(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	repo := NewWalletRepo()
	wallets, err := repo.GetForUpdate(ctx, tx, a.ID, "00000000-0000-0000-0000-000000000000")
	if err != nil {
		t.Fatalf("GetForUpdate: %v", err)
	}
	if len(wallets) != 1 {
		t.Fatalf("expected exactly 1 wallet (the other doesn't exist), got %d", len(wallets))
	}
}

func TestWalletRepo_UpdateBalance_Persists(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}

	repo := NewWalletRepo()
	newBalance := decimal.RequireFromString("42.50")
	if err := repo.UpdateBalance(ctx, tx, a.ID, newBalance); err != nil {
		t.Fatalf("UpdateBalance: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	var got decimal.Decimal
	if err := testPool.QueryRow(ctx, `SELECT balance FROM wallets WHERE id = $1`, a.ID).Scan(&got); err != nil {
		t.Fatalf("read back balance: %v", err)
	}
	if !got.Equal(newBalance) {
		t.Fatalf("expected balance %s, got %s", newBalance, got)
	}
}

// TestWalletRepo_GetForUpdate_Serializes proves FOR UPDATE actually
// blocks a second transaction from locking the same wallet until the
// first commits — the mechanism the whole concurrency guarantee rests on.
func TestWalletRepo_GetForUpdate_Serializes(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")
	repo := NewWalletRepo()

	tx1, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	if _, err := repo.GetForUpdate(ctx, tx1, a.ID); err != nil {
		t.Fatalf("tx1 GetForUpdate: %v", err)
	}

	unblocked := make(chan struct{})
	go func() {
		tx2, err := testPool.Begin(ctx)
		if err != nil {
			t.Errorf("begin tx2: %v", err)
			return
		}
		defer func() { _ = tx2.Rollback(ctx) }()

		if _, err := repo.GetForUpdate(ctx, tx2, a.ID); err != nil {
			t.Errorf("tx2 GetForUpdate: %v", err)
			return
		}
		close(unblocked)
	}()

	select {
	case <-unblocked:
		t.Fatalf("tx2 acquired the lock while tx1 still held it")
	case <-time.After(300 * time.Millisecond):
		// expected: tx2 is still blocked
	}

	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit tx1: %v", err)
	}

	select {
	case <-unblocked:
		// expected: tx2 proceeds once tx1 releases the lock
	case <-time.After(2 * time.Second):
		t.Fatalf("tx2 never acquired the lock after tx1 committed")
	}
}
