package domain

import (
	"math"
	"testing"
	"time"
)

func TestTransferValidation(t *testing.T) {
	for _, tc := range []struct {
		name, from, to string
		amount         int64
		valid          bool
	}{
		{"valid", "a", "b", 1, true},
		{"largest exact amount", "a", "b", math.MaxInt64, true},
		{"zero", "a", "b", 0, false},
		{"negative", "a", "b", -1, false},
		{"same wallet", "a", "a", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			at := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
			transfer, err := NewTransfer("t1", "key", tc.from, tc.to, tc.amount, at)
			if (err == nil) != tc.valid {
				t.Fatalf("NewTransfer error = %v, valid = %v", err, tc.valid)
			}
			if !tc.valid {
				return
			}
			if err := transfer.Validate(); err != nil {
				t.Fatal(err)
			}
			if transfer.ID != "t1" || transfer.IdempotencyKey != "key" || transfer.FromWalletID != tc.from || transfer.ToWalletID != tc.to || transfer.Amount != tc.amount || transfer.Status() != TransferStatusPending || transfer.CreatedAt != at || transfer.UpdatedAt != at {
				t.Fatalf("unexpected transfer: %+v", transfer)
			}
		})
	}
	var zero Transfer
	if err := zero.Validate(); err == nil {
		t.Fatal("zero-value transfer must be invalid")
	}
}

func TestTransferStatuses(t *testing.T) {
	for _, tc := range []struct {
		status TransferStatus
		valid  bool
	}{
		{TransferStatusPending, true}, {TransferStatusProcessed, true}, {TransferStatusFailed, true},
		{"", false}, {"processed", false}, {"UNKNOWN", false},
	} {
		if (tc.status.Validate() == nil) != tc.valid {
			t.Errorf("unexpected validation for %q", tc.status)
		}
	}
	if string(TransferStatusPending) != "PENDING" || string(TransferStatusProcessed) != "PROCESSED" || string(TransferStatusFailed) != "FAILED" {
		t.Fatal("status values must match the contract")
	}
}

func TestTransferTransitions(t *testing.T) {
	created := time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC)
	updated := created.Add(time.Second)
	for _, initial := range []TransferStatus{TransferStatusPending, TransferStatusProcessed, TransferStatusFailed, "", "UNKNOWN"} {
		for _, target := range []TransferStatus{TransferStatusProcessed, TransferStatusFailed} {
			t.Run(string(initial)+" to "+string(target), func(t *testing.T) {
				transfer := Transfer{ID: "t1", FromWalletID: "a", ToWalletID: "b", Amount: 10, status: initial, CreatedAt: created, UpdatedAt: created}
				before := transfer
				var err error
				if target == TransferStatusProcessed {
					err = transfer.MarkProcessed(updated)
				} else {
					err = transfer.MarkFailed(updated)
				}
				if initial != TransferStatusPending {
					if err == nil {
						t.Fatal("invalid transition accepted")
					}
					if transfer != before {
						t.Fatal("rejected transition mutated transfer")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if transfer.Status() != target || transfer.UpdatedAt != updated {
					t.Fatalf("unexpected result: %+v", transfer)
				}
				before.status = target
				before.UpdatedAt = updated
				if transfer != before {
					t.Fatal("transition changed unrelated fields")
				}
			})
		}
	}
}

func TestLedgerEntryValidation(t *testing.T) {
	for _, tc := range []struct {
		kind   LedgerEntryType
		amount int64
		valid  bool
	}{
		{LedgerEntryDebit, 1, true}, {LedgerEntryCredit, math.MaxInt64, true},
		{LedgerEntryDebit, 0, false}, {LedgerEntryCredit, -1, false},
		{"", 1, false}, {"debit", 1, false}, {"UNKNOWN", 1, false},
	} {
		entry := LedgerEntry{ID: "e1", WalletID: "a", TransferID: "t1", Type: tc.kind, Amount: tc.amount, CreatedAt: time.Now()}
		if (entry.Validate() == nil) != tc.valid {
			t.Errorf("unexpected validation for %+v", entry)
		}
	}
	if string(LedgerEntryDebit) != "DEBIT" || string(LedgerEntryCredit) != "CREDIT" {
		t.Fatal("ledger types must match the contract")
	}
}
