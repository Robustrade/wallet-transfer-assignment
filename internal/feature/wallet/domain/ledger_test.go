package domain_test

import (
	"errors"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
)

func TestNewLedgerEntries_ProducesBalancedDebitCreditPair(t *testing.T) {
	entries, err := domain.NewLedgerEntries(7, 1, 2, 250)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want exactly 2", len(entries))
	}

	debit, credit := entries[0], entries[1]

	if debit.EntryType != domain.LedgerDebit || debit.WalletID != 1 {
		t.Errorf("first entry should debit sender, got %+v", debit)
	}
	if credit.EntryType != domain.LedgerCredit || credit.WalletID != 2 {
		t.Errorf("second entry should credit receiver, got %+v", credit)
	}
	if debit.Amount != credit.Amount || debit.Amount != 250 {
		t.Errorf("ledger does not balance: debit=%d credit=%d", debit.Amount, credit.Amount)
	}
	if debit.TransferID != 7 || credit.TransferID != 7 {
		t.Errorf("entries must reference the transfer, got %d/%d", debit.TransferID, credit.TransferID)
	}
	for _, e := range entries {
		if err := e.Validate(); err != nil {
			t.Errorf("generated entry failed validation: %v", err)
		}
	}
}

func TestNewLedgerEntries_Rejects(t *testing.T) {
	tests := []struct {
		name                         string
		transferID, from, to, amount int64
		wantErr                      error
	}{
		{"transfer id zero (unsaved transfer)", 0, 1, 2, 10, domain.ErrInvalidLedgerEntry},
		{"sender zero", 1, 0, 2, 10, domain.ErrInvalidLedgerEntry},
		{"receiver zero", 1, 1, 0, 10, domain.ErrInvalidLedgerEntry},
		{"same wallet", 1, 3, 3, 10, domain.ErrInvalidLedgerEntry},
		{"zero amount", 1, 1, 2, 0, domain.ErrInvalidAmount},
		{"negative amount", 1, 1, 2, -10, domain.ErrInvalidAmount},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries, err := domain.NewLedgerEntries(tc.transferID, tc.from, tc.to, tc.amount)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
			if entries != nil {
				t.Fatalf("expected no entries on error, got %+v", entries)
			}
		})
	}
}

func TestLedgerEntryValidate(t *testing.T) {
	valid := domain.LedgerEntry{TransferID: 1, WalletID: 1, EntryType: domain.LedgerDebit, Amount: 1}

	if err := valid.Validate(); err != nil {
		t.Fatalf("valid entry rejected: %v", err)
	}

	bad := valid
	bad.EntryType = "SIDEWAYS"
	if err := bad.Validate(); !errors.Is(err, domain.ErrInvalidLedgerEntry) {
		t.Errorf("unknown type: got %v", err)
	}

	bad = valid
	bad.Amount = 0
	if err := bad.Validate(); !errors.Is(err, domain.ErrInvalidAmount) {
		t.Errorf("zero amount: got %v", err)
	}

	bad = valid
	bad.TransferID = 0
	if err := bad.Validate(); !errors.Is(err, domain.ErrInvalidLedgerEntry) {
		t.Errorf("missing transfer id: got %v", err)
	}
}
