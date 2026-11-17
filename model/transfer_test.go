package model_test

import (
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

func TestNewTransfer_StartsPending(t *testing.T) {
	t.Parallel()
	tr, err := model.NewTransfer("key-1", "wallet_a", "wallet_b", 100)
	if err != nil {
		t.Fatal(err)
	}
	if tr.Status != model.StatusPending {
		t.Fatalf("status=%s", tr.Status)
	}
	if tr.FailureReason != nil {
		t.Fatal("failure reason should be nil")
	}
	if tr.FromWalletID != "wallet_a" || tr.ToWalletID != "wallet_b" {
		t.Fatalf("wallets %s -> %s", tr.FromWalletID, tr.ToWalletID)
	}
}

func TestNewTransfer_RejectsSameWalletAndBadAmount(t *testing.T) {
	t.Parallel()
	if _, err := model.NewTransfer("k", "w1", "w1", 10); err != model.ErrSameWallet {
		t.Fatalf("same wallet: %v", err)
	}
	if _, err := model.NewTransfer("k", "w1", "w2", 0); err != model.ErrInvalidAmount {
		t.Fatalf("zero amount: %v", err)
	}
	if _, err := model.NewTransfer("  ", "w1", "w2", 10); err != model.ErrInvalidIdempotencyKey {
		t.Fatalf("empty key: %v", err)
	}
}

func TestTransfer_MarkProcessed(t *testing.T) {
	t.Parallel()
	tr, _ := model.NewTransfer("k", "a", "b", 10)
	if err := tr.MarkProcessed(); err != nil {
		t.Fatal(err)
	}
	if tr.Status != model.StatusProcessed || tr.FailureReason != nil {
		t.Fatalf("status=%s reason=%v", tr.Status, tr.FailureReason)
	}
	if err := tr.MarkProcessed(); err != model.ErrInvalidTransition {
		t.Fatalf("second mark: %v", err)
	}
	if err := tr.MarkFailed(model.FailureInsufficientFunds); err != model.ErrInvalidTransition {
		t.Fatalf("failed after processed: %v", err)
	}
	if tr.Status != model.StatusProcessed {
		t.Fatal("status must stay PROCESSED")
	}
}

func TestTransfer_MarkFailed(t *testing.T) {
	t.Parallel()
	tr, _ := model.NewTransfer("k", "a", "b", 10)
	if err := tr.MarkFailed(model.FailureInsufficientFunds); err != nil {
		t.Fatal(err)
	}
	if tr.Status != model.StatusFailed {
		t.Fatalf("status=%s", tr.Status)
	}
	if tr.FailureReason == nil || *tr.FailureReason != model.FailureInsufficientFunds {
		t.Fatalf("reason=%v", tr.FailureReason)
	}
	if err := tr.MarkProcessed(); err != model.ErrInvalidTransition {
		t.Fatalf("processed after failed: %v", err)
	}
	if err := tr.MarkFailed("other"); err != model.ErrInvalidTransition {
		t.Fatalf("failed again: %v", err)
	}
}

func TestTransfer_MarkFailed_EmptyReason(t *testing.T) {
	t.Parallel()
	tr, _ := model.NewTransfer("k", "a", "b", 10)
	if err := tr.MarkFailed("  "); err != model.ErrInvalidFailureReason {
		t.Fatalf("got %v", err)
	}
	if tr.Status != model.StatusPending {
		t.Fatal("status must remain PENDING")
	}
}
