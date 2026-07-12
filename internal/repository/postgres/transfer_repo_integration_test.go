//go:build integration

package postgres

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/idgen"
)

var testIDGen = mustIDGenerator()

func mustIDGenerator() *idgen.Generator {
	gen, err := idgen.NewGenerator(0)
	if err != nil {
		panic(err)
	}
	return gen
}

func nextTestTransferID(t *testing.T) int64 {
	t.Helper()
	id, err := testIDGen.NextID()
	if err != nil {
		t.Fatalf("mint test transfer id: %v", err)
	}
	return id
}

func TestTransferRepo_InsertTransfer_Success(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")
	b := newTestWallet(t, "0.00")
	repo := NewTransferRepo(testPool)
	id := nextTestTransferID(t)

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	transfer, err := repo.InsertTransfer(ctx, tx, id, "hash-1", a.ID, b.ID, decimal.RequireFromString("5.00"), domain.StatusProcessed, nil)
	if err != nil {
		t.Fatalf("InsertTransfer: %v", err)
	}
	if transfer == nil {
		t.Fatalf("expected a claimed transfer, got nil")
	}
	if transfer.ID != id || transfer.RequestHash != "hash-1" || transfer.Status != domain.StatusProcessed {
		t.Fatalf("unexpected transfer: %+v", transfer)
	}
}

func TestTransferRepo_InsertTransfer_FailedPersistsReason(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")
	b := newTestWallet(t, "0.00")
	repo := NewTransferRepo(testPool)
	id := nextTestTransferID(t)

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	reason := domain.ErrCodeInsufficientBalance
	transfer, err := repo.InsertTransfer(ctx, tx, id, "hash-1", a.ID, b.ID, decimal.RequireFromString("5.00"), domain.StatusFailed, &reason)
	if err != nil {
		t.Fatalf("InsertTransfer: %v", err)
	}
	if transfer.Status != domain.StatusFailed || transfer.FailureReason == nil || *transfer.FailureReason != domain.ErrCodeInsufficientBalance {
		t.Fatalf("unexpected transfer: %+v", transfer)
	}
}

func TestTransferRepo_InsertTransfer_ConflictReturnsNil(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")
	b := newTestWallet(t, "0.00")
	repo := NewTransferRepo(testPool)
	id := nextTestTransferID(t)

	tx1, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	if _, err := repo.InsertTransfer(ctx, tx1, id, "hash-1", a.ID, b.ID, decimal.RequireFromString("5.00"), domain.StatusProcessed, nil); err != nil {
		t.Fatalf("tx1 InsertTransfer: %v", err)
	}
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("commit tx1: %v", err)
	}

	tx2, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx2: %v", err)
	}
	defer func() { _ = tx2.Rollback(ctx) }()

	transfer, err := repo.InsertTransfer(ctx, tx2, id, "hash-2", a.ID, b.ID, decimal.RequireFromString("7.00"), domain.StatusProcessed, nil)
	if err != nil {
		t.Fatalf("tx2 InsertTransfer: %v", err)
	}
	if transfer != nil {
		t.Fatalf("expected nil (conflict), got %+v", transfer)
	}
}

func TestTransferRepo_InsertTransfer_RejectsUnknownWallet(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")
	repo := NewTransferRepo(testPool)
	id := nextTestTransferID(t)

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	_, err = repo.InsertTransfer(ctx, tx, id, "hash-1", a.ID, "00000000-0000-0000-0000-000000000000", decimal.RequireFromString("5.00"), domain.StatusProcessed, nil)
	if err == nil {
		t.Fatalf("expected the FK constraint to reject an unknown wallet id")
	}
}

func TestTransferRepo_FindByID(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")
	b := newTestWallet(t, "0.00")
	repo := NewTransferRepo(testPool)
	id := nextTestTransferID(t)

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if _, err := repo.InsertTransfer(ctx, tx, id, "hash-1", a.ID, b.ID, decimal.RequireFromString("5.00"), domain.StatusProcessed, nil); err != nil {
		t.Fatalf("InsertTransfer: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	found, err := repo.FindByID(ctx, id)
	if err != nil {
		t.Fatalf("FindByID: %v", err)
	}
	if found == nil || found.Status != domain.StatusProcessed {
		t.Fatalf("expected a PROCESSED transfer, got %+v", found)
	}

	notFound, err := repo.FindByID(ctx, id+999999)
	if err != nil {
		t.Fatalf("FindByID (missing): %v", err)
	}
	if notFound != nil {
		t.Fatalf("expected nil for a missing transfer, got %+v", notFound)
	}
}

func TestTransferRepo_InsertLedgerEntries_ExactlyTwoRows(t *testing.T) {
	ctx := context.Background()
	a := newTestWallet(t, "10.00")
	b := newTestWallet(t, "0.00")
	repo := NewTransferRepo(testPool)
	id := nextTestTransferID(t)
	amount := decimal.RequireFromString("5.00")

	tx, err := testPool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := repo.InsertTransfer(ctx, tx, id, "hash-1", a.ID, b.ID, amount, domain.StatusProcessed, nil); err != nil {
		t.Fatalf("InsertTransfer: %v", err)
	}
	if err := repo.InsertLedgerEntries(ctx, tx, id, a.ID, b.ID, amount); err != nil {
		t.Fatalf("InsertLedgerEntries: %v", err)
	}

	var count int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, id).Scan(&count); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", count)
	}

	var debitCount, creditCount int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transfer_id = $1 AND entry_type = 'DEBIT' AND wallet_id = $2 AND amount = $3`, id, a.ID, amount).Scan(&debitCount); err != nil {
		t.Fatalf("count debit entries: %v", err)
	}
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transfer_id = $1 AND entry_type = 'CREDIT' AND wallet_id = $2 AND amount = $3`, id, b.ID, amount).Scan(&creditCount); err != nil {
		t.Fatalf("count credit entries: %v", err)
	}
	if debitCount != 1 || creditCount != 1 {
		t.Fatalf("expected 1 DEBIT on %s and 1 CREDIT on %s, got debit=%d credit=%d", a.ID, b.ID, debitCount, creditCount)
	}
}
