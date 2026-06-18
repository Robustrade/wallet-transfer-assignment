package service

import (
	"context"
	"errors"
	"testing"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/domain"
)

func TestCreateTransfer_Success(t *testing.T) {
	store := newFakeStore()
	store.seedWallet("wallet_1", 500)
	store.seedWallet("wallet_2", 0)
	svc := NewTransferService(store)

	tr, _, err := svc.CreateTransfer(context.Background(), TransferRequest{
		IdempotencyKey: "key1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if tr.Status != domain.StatusProcessed {
		t.Errorf("status = %s, want PROCESSED", tr.Status)
	}
	if store.wallets["wallet_1"].Balance != 400 {
		t.Errorf("source balance = %d, want 400", store.wallets["wallet_1"].Balance)
	}
	if store.wallets["wallet_2"].Balance != 100 {
		t.Errorf("dest balance = %d, want 100", store.wallets["wallet_2"].Balance)
	}
	if len(store.ledger) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(store.ledger))
	}
}

func TestCreateTransfer_LedgerIsBalanced(t *testing.T) {
	store := newFakeStore()
	store.seedWallet("wallet_1", 500)
	store.seedWallet("wallet_2", 0)
	svc := NewTransferService(store)

	_, _, err := svc.CreateTransfer(context.Background(), TransferRequest{
		IdempotencyKey: "key1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var debits, credits int
	for _, e := range store.ledger {
		switch e.Type {
		case domain.EntryDebit:
			debits++
			if e.WalletID != "wallet_1" {
				t.Errorf("debit on wrong wallet: %s", e.WalletID)
			}
		case domain.EntryCredit:
			credits++
			if e.WalletID != "wallet_2" {
				t.Errorf("credit on wrong wallet: %s", e.WalletID)
			}
		}
	}
	if debits != 1 || credits != 1 {
		t.Errorf("debits=%d credits=%d, want 1 and 1", debits, credits)
	}
}

func TestCreateTransfer_Idempotent(t *testing.T) {
	store := newFakeStore()
	store.seedWallet("wallet_1", 500)
	store.seedWallet("wallet_2", 0)
	svc := NewTransferService(store)

	req := TransferRequest{
		IdempotencyKey: "same-key",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, _, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	second, _, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	if first.ID != second.ID {
		t.Errorf("transfer IDs differ: %s vs %s", first.ID, second.ID)
	}
	// Money moved only once.
	if store.wallets["wallet_1"].Balance != 400 {
		t.Errorf("source balance = %d, want 400 (charged once)", store.wallets["wallet_1"].Balance)
	}
	if len(store.transfers) != 1 {
		t.Errorf("transfers created = %d, want 1", len(store.transfers))
	}
	if len(store.ledger) != 2 {
		t.Errorf("ledger entries = %d, want 2", len(store.ledger))
	}
}

func TestCreateTransfer_InsufficientBalance(t *testing.T) {
	store := newFakeStore()
	store.seedWallet("wallet_1", 50)
	store.seedWallet("wallet_2", 0)
	svc := NewTransferService(store)

	_, _, err := svc.CreateTransfer(context.Background(), TransferRequest{
		IdempotencyKey: "key1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if !errors.Is(err, ErrInsufficientBalance) {
		t.Fatalf("error = %v, want ErrInsufficientBalance", err)
	}
	// No money moved.
	if store.wallets["wallet_1"].Balance != 50 {
		t.Errorf("source balance = %d, want 50 (unchanged)", store.wallets["wallet_1"].Balance)
	}
	if len(store.ledger) != 0 {
		t.Errorf("ledger entries = %d, want 0", len(store.ledger))
	}
}

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	store := newFakeStore()
	store.seedWallet("wallet_1", 500)
	svc := NewTransferService(store)

	tests := []struct {
		name    string
		req     TransferRequest
		wantErr error
	}{
		{"same wallet", TransferRequest{"k", "wallet_1", "wallet_1", 100}, domain.ErrSameWallet},
		{"zero amount", TransferRequest{"k", "wallet_1", "wallet_2", 0}, domain.ErrInvalidAmount},
		{"negative amount", TransferRequest{"k", "wallet_1", "wallet_2", -5}, domain.ErrInvalidAmount},
		{"empty from", TransferRequest{"k", "", "wallet_2", 100}, domain.ErrMissingWallet},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := svc.CreateTransfer(context.Background(), tt.req)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("error = %v, want %v", err, tt.wantErr)
			}
		})
	}
}
