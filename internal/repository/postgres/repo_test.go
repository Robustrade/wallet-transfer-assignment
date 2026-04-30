package postgres

import (
	"context"
	"testing"

	"github.com/gerry1818/wallet-transfer-assignment/internal/model"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&model.Wallet{}, &model.Transfer{}, &model.LedgerEntry{}, &model.IdempotencyRecord{}); err != nil {
		t.Fatalf("failed to migrate schema: %v", err)
	}
	return db
}

func TestRepoIdempotencyAndTx(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	repo := NewRepo(db)

	if err := db.Create(&model.Wallet{ID: 1, Balance: 1000}).Error; err != nil {
		t.Fatalf("failed to seed wallet 1: %v", err)
	}
	if err := db.Create(&model.Wallet{ID: 2, Balance: 500}).Error; err != nil {
		t.Fatalf("failed to seed wallet 2: %v", err)
	}

	ok, err := repo.ClaimIdempotency(ctx, "k1", "h1")
	if err != nil || !ok {
		t.Fatalf("expected first claim to succeed, got ok=%v err=%v", ok, err)
	}

	ok, err = repo.ClaimIdempotency(ctx, "k1", "h1")
	if err != nil || ok {
		t.Fatalf("expected duplicate claim with same hash to be non-owner")
	}

	ok, err = repo.ClaimIdempotency(ctx, "k1", "h2")
	if err == nil || ok {
		t.Fatalf("expected duplicate claim with different hash to fail")
	}

	if err := repo.UpdateIdempotency(ctx, "k1", "COMPLETED", `{"TransferID":1,"Status":"PROCESSED"}`, 200); err != nil {
		t.Fatalf("update idempotency failed: %v", err)
	}

	payload, code, err := repo.GetIdempotency(ctx, "k1")
	if err != nil {
		t.Fatalf("get idempotency failed: %v", err)
	}
	if payload == "" || code != 200 {
		t.Fatalf("unexpected idempotency payload/code")
	}

	txRaw, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}

	bal1, err := txRaw.GetBalance(ctx, 1)
	if err != nil || bal1 != 1000 {
		t.Fatalf("unexpected balance for wallet 1")
	}

	bal2, err := txRaw.GetWalletForUpdate(ctx, 2)
	if err != nil || bal2 != 500 {
		t.Fatalf("unexpected balance for wallet 2")
	}

	tid, err := txRaw.CreateTransfer(ctx, 1, 2, 200, "k2")
	if err != nil || tid == 0 {
		t.Fatalf("failed to create transfer")
	}

	if err := txRaw.UpdateWallet(ctx, 1, 800); err != nil {
		t.Fatalf("failed to update wallet 1: %v", err)
	}
	if err := txRaw.UpdateWallet(ctx, 2, 700); err != nil {
		t.Fatalf("failed to update wallet 2: %v", err)
	}
	if err := txRaw.InsertLedgerEntry(ctx, 1, tid, "DEBIT", 200); err != nil {
		t.Fatalf("failed to insert debit ledger: %v", err)
	}
	if err := txRaw.InsertLedgerEntry(ctx, 2, tid, "CREDIT", 200); err != nil {
		t.Fatalf("failed to insert credit ledger: %v", err)
	}
	if err := txRaw.UpdateTransferState(ctx, tid, "PROCESSED"); err != nil {
		t.Fatalf("failed to update transfer state: %v", err)
	}

	if err := txRaw.Commit(ctx); err != nil {
		t.Fatalf("commit failed: %v", err)
	}
}

func TestTxRollback(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	repo := NewRepo(db)
	txRaw, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}
	if err := txRaw.Rollback(ctx); err != nil {
		t.Fatalf("rollback failed: %v", err)
	}
}

func TestRepoGetIdempotencyNotFound(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	repo := NewRepo(db)

	_, _, err := repo.GetIdempotency(ctx, "missing-key")
	if err == nil {
		t.Fatalf("expected not found error")
	}
}

func TestTxCreateTransferError(t *testing.T) {
	ctx := context.Background()
	db := setupTestDB(t)
	repo := NewRepo(db)

	txRaw, err := repo.BeginTx(ctx)
	if err != nil {
		t.Fatalf("begin tx failed: %v", err)
	}

	if _, err := txRaw.CreateTransfer(ctx, 1, 2, 50, "dup-key"); err != nil {
		t.Fatalf("first create transfer should succeed: %v", err)
	}
	if _, err := txRaw.CreateTransfer(ctx, 1, 2, 50, "dup-key"); err == nil {
		t.Fatalf("expected duplicate idempotency key to fail")
	}
	_ = txRaw.Rollback(ctx)
}
