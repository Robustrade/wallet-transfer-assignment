package domain

import (
	"errors"
	"testing"
)

func TestTransferStateTransitions(t *testing.T) {
	money, err := NewMoney(100, "USD")
	if err != nil {
		t.Fatalf("NewMoney returned error: %v", err)
	}
	transfer, err := NewTransfer("key-1", "wallet-a", "wallet-b", money)
	if err != nil {
		t.Fatalf("NewTransfer returned error: %v", err)
	}

	if err := transfer.MarkProcessed(); err != nil {
		t.Fatalf("MarkProcessed returned error: %v", err)
	}
	if transfer.Status != TransferStatusProcessed {
		t.Fatalf("status = %q, want %q", transfer.Status, TransferStatusProcessed)
	}
	if err := transfer.MarkFailed(FailureInsufficientFunds); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("err = %v, want %v", err, ErrInvalidState)
	}
}

func TestNewTransferRejectsSameWallet(t *testing.T) {
	money, err := NewMoney(100, "USD")
	if err != nil {
		t.Fatalf("NewMoney returned error: %v", err)
	}

	_, err = NewTransfer("key-1", "wallet-a", "wallet-a", money)
	if !errors.Is(err, ErrSameWallet) {
		t.Fatalf("err = %v, want %v", err, ErrSameWallet)
	}
}
