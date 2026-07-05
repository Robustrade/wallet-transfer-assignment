//go:build integration

package repository_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/repository"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/testutil"
)

func TestLedgerRepository_InsertLedgerEntries(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	transferRepo := repository.NewTransferRepository()
	transfer, err := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := transferRepo.InsertTransfer(context.Background(), pool, transfer); err != nil {
		t.Fatalf("insert transfer: %v", err)
	}

	debit, credit := domain.NewLedgerPair(transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
	repo := repository.NewLedgerRepository()
	if err := repo.InsertLedgerEntries(context.Background(), pool, debit, credit); err != nil {
		t.Fatalf("insert ledger entries: %v", err)
	}

	var count int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, transfer.ID).Scan(&count); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if count != 2 {
		t.Errorf("expected 2 ledger entries, got %d", count)
	}
}

// TestLedgerRepository_InsertLedgerEntries_DuplicateTypeViolatesUnique
// confirms the (transfer_id, type) unique constraint -- the invariant that
// a transfer can never end up with two DEBITs (or two CREDITs) -- is
// actually enforced by the schema, not just by application-level care.
func TestLedgerRepository_InsertLedgerEntries_DuplicateTypeViolatesUnique(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	transferRepo := repository.NewTransferRepository()
	transfer, err := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := transferRepo.InsertTransfer(context.Background(), pool, transfer); err != nil {
		t.Fatalf("insert transfer: %v", err)
	}

	debit, _ := domain.NewLedgerPair(transfer.ID, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount)
	repo := repository.NewLedgerRepository()
	if err := repo.InsertLedgerEntries(context.Background(), pool, debit); err != nil {
		t.Fatalf("insert first debit: %v", err)
	}

	duplicateDebit := debit
	duplicateDebit.ID = uuid.New()
	if err := repo.InsertLedgerEntries(context.Background(), pool, duplicateDebit); err == nil {
		t.Fatal("expected unique violation inserting a second DEBIT for the same transfer, got nil")
	}
}
