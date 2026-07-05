//go:build integration

package service

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/repository"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/testutil"
)

// TestCreateTransfer_ResumesPendingTransfer seeds a transfer + idempotency
// record directly, as if a prior request had crashed after creating them
// but before locking wallets and driving the transfer to a terminal state.
// A retry with the same idempotencyKey must pick the PENDING transfer back
// up and complete it, rather than erroring or creating a second transfer.
//
// This lives in package service (not service_test) because it needs
// computeRequestHash to seed a request_hash the retry will actually match
// -- that function is intentionally unexported.
func TestCreateTransfer_ResumesPendingTransfer(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 500)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	req := CreateTransferRequest{
		IdempotencyKey: "key-resume",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         150,
	}

	transferID := uuid.New()
	pendingTransfer, err := domain.NewPendingTransfer(transferID, req.FromWalletID, req.ToWalletID, req.Amount)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	transferRepo := repository.NewTransferRepository()
	if err := transferRepo.InsertTransfer(context.Background(), pool, pendingTransfer); err != nil {
		t.Fatalf("seed pending transfer: %v", err)
	}

	idempotencyRepo := repository.NewIdempotencyRepository()
	idempotencyRecord := domain.IdempotencyRecord{
		IdempotencyKey: req.IdempotencyKey,
		RequestHash:    computeRequestHash(req.FromWalletID, req.ToWalletID, req.Amount),
		TransferID:     transferID,
		CreatedAt:      pendingTransfer.CreatedAt,
	}
	if err := idempotencyRepo.InsertIdempotencyKey(context.Background(), pool, idempotencyRecord); err != nil {
		t.Fatalf("seed idempotency record: %v", err)
	}

	svc := NewTransferService(repository.NewStore(pool))
	transfer, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error resuming pending transfer: %v", err)
	}
	if transfer.ID != transferID {
		t.Errorf("expected resumed transfer ID %s, got %s", transferID, transfer.ID)
	}
	if transfer.Status != domain.StatusProcessed {
		t.Errorf("expected status PROCESSED, got %s", transfer.Status)
	}

	var fromBalance int64
	if err := pool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE id = $1`, "wallet_1").Scan(&fromBalance); err != nil {
		t.Fatalf("query balance: %v", err)
	}
	if fromBalance != 350 {
		t.Errorf("expected wallet_1 debited exactly once to 350, got %d", fromBalance)
	}
}
