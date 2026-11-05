package repository_test

import (
	"context"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/testutil"
)

// Regression test for a deadlock found under load.
//
// Inserting a transfer makes Postgres take FOR KEY SHARE locks on both wallets
// (foreign-key check). If LockByIDs used FOR UPDATE, that insert would wait for the
// wallet lock, and a B->A insert could deadlock with a transfer holding wallet A.
// The wallet lock must therefore never block a foreign-key check.
func TestLockByIDs_DoesNotBlockForeignKeyChecks(t *testing.T) {
	conn := testutil.DB(t)
	testutil.SeedWallet(t, conn, "A", 100)
	testutil.SeedWallet(t, conn, "B", 100)
	ctx := context.Background()

	holder, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = holder.Rollback() }()
	if _, err := (repository.WalletRepository{}).LockByIDs(ctx, holder, []string{"A"}); err != nil {
		t.Fatal(err)
	}

	other, err := conn.BeginTx(ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = other.Rollback() }()
	if _, err := other.ExecContext(ctx, `SET LOCAL lock_timeout = '500ms'`); err != nil {
		t.Fatal(err)
	}

	_, err = (repository.TransferRepository{}).InsertPending(ctx, other,
		"22222222-2222-4222-8222-222222222222", "B", "A", 1)
	if err != nil {
		t.Fatalf("inserting a transfer was blocked by the wallet lock: %v", err)
	}
}
