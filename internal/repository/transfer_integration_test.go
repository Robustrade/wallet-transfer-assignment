//go:build integration

package repository_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/repository"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/testutil"
)

func TestTransferRepository_InsertAndGet(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	repo := repository.NewTransferRepository()
	transfer, err := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if err := repo.InsertTransfer(context.Background(), pool, transfer); err != nil {
		t.Fatalf("insert transfer: %v", err)
	}

	got, err := repo.GetTransferByID(context.Background(), pool, transfer.ID)
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if got.Status != domain.StatusPending {
		t.Errorf("expected status PENDING, got %s", got.Status)
	}
	if got.Amount != 50 {
		t.Errorf("expected amount 50, got %d", got.Amount)
	}
	if got.FromWalletID != "wallet_1" || got.ToWalletID != "wallet_2" {
		t.Errorf("expected wallet_1->wallet_2, got %s->%s", got.FromWalletID, got.ToWalletID)
	}
}

func TestTransferRepository_GetTransferByID_NotFound(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)

	repo := repository.NewTransferRepository()
	_, err := repo.GetTransferByID(context.Background(), pool, uuid.New())
	if !errors.Is(err, domain.ErrTransferNotFound) {
		t.Errorf("expected ErrTransferNotFound, got %v", err)
	}
}

func TestTransferRepository_UpdateTransferStatus(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	repo := repository.NewTransferRepository()
	transfer, err := domain.NewPendingTransfer(uuid.New(), "wallet_1", "wallet_2", 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := repo.InsertTransfer(context.Background(), pool, transfer); err != nil {
		t.Fatalf("insert transfer: %v", err)
	}

	reason := "insufficient balance"
	if err := repo.UpdateTransferStatus(context.Background(), pool, transfer.ID, domain.StatusFailed, &reason); err != nil {
		t.Fatalf("update status: %v", err)
	}

	got, err := repo.GetTransferByID(context.Background(), pool, transfer.ID)
	if err != nil {
		t.Fatalf("get transfer: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Errorf("expected status FAILED, got %s", got.Status)
	}
	if got.FailureReason == nil || *got.FailureReason != reason {
		t.Errorf("expected failure reason %q, got %v", reason, got.FailureReason)
	}
}

func TestTransferRepository_LockForUpdate_NotFound(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)

	repo := repository.NewTransferRepository()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	defer tx.Rollback(ctx)

	_, err = repo.LockForUpdate(ctx, tx, uuid.New())
	if !errors.Is(err, domain.ErrTransferNotFound) {
		t.Errorf("expected ErrTransferNotFound, got %v", err)
	}
}

// TestTransferRepository_LockForUpdate_BlocksConcurrentTx confirms the
// transfer-row FOR UPDATE lock actually blocks a second transaction --
// this is what serializes two concurrent replays of the same
// idempotencyKey so only one of them drives the transfer to a terminal
// state.
func TestTransferRepository_LockForUpdate_BlocksConcurrentTx(t *testing.T) {
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

	ctx := context.Background()
	tx1, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx1: %v", err)
	}
	defer tx1.Rollback(ctx)

	if _, err := transferRepo.LockForUpdate(ctx, tx1, transfer.ID); err != nil {
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
		if _, err := transferRepo.LockForUpdate(ctx, tx2, transfer.ID); err != nil {
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

// TestTransferRepository_InsertTransfer_RejectsSelfTransfer proves the
// transfers CHECK (from_wallet_id <> to_wallet_id) constraint is enforced
// by Postgres itself, independent of domain.NewPendingTransfer's own guard
// against self-transfers -- this bypasses that guard by constructing the
// domain.Transfer directly.
func TestTransferRepository_InsertTransfer_RejectsSelfTransfer(t *testing.T) {
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	testutil.SeedWallet(t, pool, "wallet_1", 100)

	repo := repository.NewTransferRepository()
	now := time.Now()
	selfTransfer := domain.Transfer{
		ID:           uuid.New(),
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_1",
		Amount:       50,
		Status:       domain.StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	err := repo.InsertTransfer(context.Background(), pool, selfTransfer)
	if err == nil {
		t.Fatal("expected the from_wallet_id <> to_wallet_id CHECK constraint to reject a self-transfer, got nil")
	}
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23514" {
		t.Errorf("expected a check_violation (23514), got %v", err)
	}
}
