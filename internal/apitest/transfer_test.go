package apitest

import (
	"net/http"
	"slices"
	"testing"

	"github.com/google/uuid"
)

func TestTransferWithSufficientFunds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name            string
		fromBalance     int64
		amount          int64
		wantFromBalance int64
	}{
		{"part of the balance", 1000, 300, 700},
		{"the whole balance", 1000, 1000, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beginTest(t)
			from := createWallet(t, tt.fromBalance)
			to := createWallet(t, 500)
			key := newKey()

			got := postTransfer(t, transferBody(key, from, to, tt.amount)).transfer(t, http.StatusCreated)

			if got.State != "PROCESSED" {
				t.Errorf("state = %q, want PROCESSED", got.State)
			}
			if id, err := uuid.Parse(got.TransferID); err != nil || id.Version() != 7 {
				t.Errorf("transferId = %q, want a UUIDv7", got.TransferID)
			}

			wantStored := storedTransfer{key: key, from: from, to: to, amount: tt.amount, state: "PROCESSED"}
			if stored := loadTransfer(t, got.TransferID); stored != wantStored {
				t.Errorf("stored transfer = %+v, want %+v", stored, wantStored)
			}

			if balance := balanceOf(t, from); balance != tt.wantFromBalance {
				t.Errorf("source balance = %d, want %d", balance, tt.wantFromBalance)
			}
			if balance := balanceOf(t, to); balance != 500+tt.amount {
				t.Errorf("destination balance = %d, want %d", balance, 500+tt.amount)
			}

			wantEntries := []ledgerEntry{
				{walletID: from, entryType: "DEBIT", amount: tt.amount},
				{walletID: to, entryType: "CREDIT", amount: tt.amount},
			}
			if entries := ledgerEntriesOf(t, got.TransferID); !slices.Equal(entries, wantEntries) {
				t.Errorf("ledger entries = %+v, want %+v", entries, wantEntries)
			}
		})
	}
}

func TestTransferWithInsufficientFunds(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		fromBalance int64
		amount      int64
	}{
		{"one more than the balance", 100, 101},
		{"empty wallet", 0, 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			beginTest(t)
			from := createWallet(t, tt.fromBalance)
			to := createWallet(t, 500)
			key := newKey()

			got := postTransfer(t, transferBody(key, from, to, tt.amount)).transfer(t, http.StatusUnprocessableEntity)

			if got.State != "FAILED" {
				t.Errorf("state = %q, want FAILED", got.State)
			}
			if id, err := uuid.Parse(got.TransferID); err != nil || id.Version() != 7 {
				t.Errorf("transferId = %q, want a UUIDv7", got.TransferID)
			}

			wantStored := storedTransfer{key: key, from: from, to: to, amount: tt.amount, state: "FAILED"}
			if stored := loadTransfer(t, got.TransferID); stored != wantStored {
				t.Errorf("stored transfer = %+v, want %+v", stored, wantStored)
			}

			if balance := balanceOf(t, from); balance != tt.fromBalance {
				t.Errorf("source balance = %d, want %d", balance, tt.fromBalance)
			}
			if balance := balanceOf(t, to); balance != 500 {
				t.Errorf("destination balance = %d, want 500", balance)
			}
			if entries := ledgerEntriesOf(t, got.TransferID); len(entries) != 0 {
				t.Errorf("ledger entries = %+v, want none", entries)
			}
		})
	}
}
