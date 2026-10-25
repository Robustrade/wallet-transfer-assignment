package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository/memory"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/service"
)

func setupTestService(t *testing.T) (*service.TransferService, *service.WalletService, *memory.MemoryRepository) {
	repo := memory.NewMemoryRepository()
	txService := service.NewTransferService(repo)
	wService := service.NewWalletService(repo)

	ctx := context.Background()
	_, err := wService.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_alice",
		InitialBalance: 1000,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("failed to create wallet alice: %v", err)
	}

	_, err = wService.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_bob",
		InitialBalance: 500,
		Currency:       "USD",
	})
	if err != nil {
		t.Fatalf("failed to create wallet bob: %v", err)
	}

	return txService, wService, repo
}

func TestTransferService_SuccessfulTransfer(t *testing.T) {
	txService, wService, _ := setupTestService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "idem-tx-001",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         300,
	}

	resp, status, err := txService.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if status != 201 {
		t.Fatalf("expected status 201, got %d", status)
	}
	if resp.Status != model.TransferStatusProcessed {
		t.Fatalf("expected transfer status PROCESSED, got %s", resp.Status)
	}
	if len(resp.LedgerEntries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(resp.LedgerEntries))
	}

	// Verify balances
	alice, _ := wService.GetWallet(ctx, "w_alice")
	bob, _ := wService.GetWallet(ctx, "w_bob")

	if alice.Balance != 700 {
		t.Fatalf("expected alice balance 700, got %d", alice.Balance)
	}
	if bob.Balance != 800 {
		t.Fatalf("expected bob balance 800, got %d", bob.Balance)
	}

	// Verify double-entry ledger balance
	debitFound := false
	creditFound := false
	for _, entry := range resp.LedgerEntries {
		if entry.WalletID == "w_alice" && entry.EntryType == model.EntryTypeDebit && entry.Amount == 300 {
			debitFound = true
		}
		if entry.WalletID == "w_bob" && entry.EntryType == model.EntryTypeCredit && entry.Amount == 300 {
			creditFound = true
		}
	}
	if !debitFound || !creditFound {
		t.Fatalf("ledger entries did not contain expected balanced DEBIT and CREDIT records")
	}
}

func TestTransferService_IdempotencyReplay(t *testing.T) {
	txService, wService, repo := setupTestService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "idem-tx-replay",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         200,
	}

	// First execution
	resp1, status1, err := txService.ExecuteTransfer(ctx, req)
	if err != nil || status1 != 201 {
		t.Fatalf("first transfer failed: %v, status: %d", err, status1)
	}

	// Second execution with identical payload (client retry)
	resp2, status2, err := txService.ExecuteTransfer(ctx, req)
	if err != nil {
		t.Fatalf("replay failed with error: %v", err)
	}
	if status2 != 201 {
		t.Fatalf("expected replayed status 201, got %d", status2)
	}
	if resp1.TransferID != resp2.TransferID {
		t.Fatalf("expected identical transfer ID %s, got %s", resp1.TransferID, resp2.TransferID)
	}

	// Ensure money was deducted ONLY ONCE
	alice, _ := wService.GetWallet(ctx, "w_alice")
	bob, _ := wService.GetWallet(ctx, "w_bob")

	if alice.Balance != 800 {
		t.Fatalf("expected alice balance 800 (deducted once), got %d", alice.Balance)
	}
	if bob.Balance != 700 {
		t.Fatalf("expected bob balance 700, got %d", bob.Balance)
	}

	// Verify total ledger entries for this transfer is still exactly 2
	entries, _ := repo.GetLedgerEntriesByTransferID(ctx, resp1.TransferID)
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(entries))
	}
}

func TestTransferService_IdempotencyConflictOnPayloadMismatch(t *testing.T) {
	txService, _, _ := setupTestService(t)
	ctx := context.Background()

	req1 := service.TransferRequest{
		IdempotencyKey: "idem-tx-conflict",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         100,
	}

	_, status1, err := txService.ExecuteTransfer(ctx, req1)
	if err != nil || status1 != 201 {
		t.Fatalf("first transfer failed: %v", err)
	}

	// Reusing same idempotency key with different amount
	req2 := service.TransferRequest{
		IdempotencyKey: "idem-tx-conflict",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         500, // Conflict!
	}

	_, status2, err := txService.ExecuteTransfer(ctx, req2)
	if !errors.Is(err, model.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
	if status2 != 409 {
		t.Fatalf("expected status 409 Conflict, got %d", status2)
	}
}

func TestTransferService_InsufficientFundsPersistence(t *testing.T) {
	txService, wService, repo := setupTestService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "idem-tx-overdraft",
		FromWalletID:   "w_alice", // balance 1000
		ToWalletID:     "w_bob",
		Amount:         5000, // Exceeds balance
	}

	_, status, err := txService.ExecuteTransfer(ctx, req)
	if !errors.Is(err, model.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
	if status != 409 {
		t.Fatalf("expected status 409, got %d", status)
	}

	// Balances must remain unchanged
	alice, _ := wService.GetWallet(ctx, "w_alice")
	if alice.Balance != 1000 {
		t.Fatalf("expected alice balance unchanged at 1000, got %d", alice.Balance)
	}

	// Verify failed transfer record was persisted for audit
	tRec, err := repo.GetTransferByIdempotencyKey(ctx, "idem-tx-overdraft")
	if err != nil {
		t.Fatalf("expected persisted failed transfer, got err: %v", err)
	}
	if tRec.Status != model.TransferStatusFailed {
		t.Fatalf("expected transfer status FAILED, got %s", tRec.Status)
	}

	// Verify retry deterministically returns cached 409 failure
	_, retryStatus, retryErr := txService.ExecuteTransfer(ctx, req)
	if retryStatus != 409 {
		t.Fatalf("expected cached status 409 on retry, got %d", retryStatus)
	}
	if retryErr == nil {
		t.Fatalf("expected failure error on retry, got nil")
	}
}
