package domain

import (
	"errors"
	"testing"
	"time"
)

func TestTransferStateTransitions(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	transfer := NewPendingTransfer("transfer-1", "key-1", "wallet-1", "wallet-2", 100, now)

	if err := transfer.MarkProcessed(now.Add(time.Second)); err != nil {
		t.Fatalf("mark processed: %v", err)
	}
	if transfer.Status != TransferStatusProcessed {
		t.Fatalf("status = %q, want %q", transfer.Status, TransferStatusProcessed)
	}
	if err := transfer.MarkFailed(FailureCodeInsufficientBalance, now.Add(2*time.Second)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second terminal transition error = %v, want ErrInvalidTransition", err)
	}
}

func TestFailedTransferCannotBeProcessed(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	transfer := NewPendingTransfer("transfer-1", "key-1", "wallet-1", "wallet-2", 100, now)

	if err := transfer.MarkFailed(FailureCodeInsufficientBalance, now.Add(time.Second)); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	if err := transfer.MarkProcessed(now.Add(2 * time.Second)); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("second terminal transition error = %v, want ErrInvalidTransition", err)
	}
}

func TestTransferCannotFailWithoutReason(t *testing.T) {
	transfer := NewPendingTransfer(
		"transfer-1",
		"key-1",
		"wallet-1",
		"wallet-2",
		100,
		time.Now(),
	)

	if err := transfer.MarkFailed("", time.Now()); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("error = %v, want ErrInvalidTransition", err)
	}
}
