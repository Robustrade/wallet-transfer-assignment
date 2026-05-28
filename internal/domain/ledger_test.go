package domain

import (
	"testing"

	"github.com/google/uuid"
)

// TestEntryType_Constants pins down the two valid entry types as
// the strings the database CHECK constraint expects. A typo would
// produce silent rejection at INSERT time.
func TestEntryType_Constants(t *testing.T) {
	if string(EntryDebit) != "DEBIT" {
		t.Fatalf("EntryDebit: %q", EntryDebit)
	}
	if string(EntryCredit) != "CREDIT" {
		t.Fatalf("EntryCredit: %q", EntryCredit)
	}
}

// TestLedgerEntry_ZeroValueIsValid documents the zero-value shape
// of a LedgerEntry. The repository's scan code relies on the same
// field-by-field population pattern as Wallet.
func TestLedgerEntry_ZeroValueIsValid(t *testing.T) {
	var e LedgerEntry
	if e.ID != 0 || e.WalletID != "" || e.Type != "" || e.Amount != 0 {
		t.Fatalf("zero-value drifted: %+v", e)
	}
	if e.TransferID != uuid.Nil {
		t.Fatalf("TransferID should be zero UUID: %v", e.TransferID)
	}
}

// TestLedgerEntry_AcceptsLargeAmount confirms the int64 Amount
// field has full range. Mirrors TestWallet_AcceptsMaxInt64Balance.
func TestLedgerEntry_AcceptsLargeAmount(t *testing.T) {
	e := LedgerEntry{Amount: 1<<62 - 1}
	if e.Amount != 1<<62-1 {
		t.Fatalf("amount lost precision: %d", e.Amount)
	}
}
