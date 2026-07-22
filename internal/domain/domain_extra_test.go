package domain

import "testing"

func TestEntryType_Valid(t *testing.T) {
	if !EntryDebit.Valid() || !EntryCredit.Valid() {
		t.Fatal("DEBIT/CREDIT must be valid")
	}
	if EntryType("BOGUS").Valid() {
		t.Fatal("unknown entry type must be invalid")
	}
}

func TestTransferState_ValidAndTerminal(t *testing.T) {
	for _, s := range []TransferState{StatePending, StateProcessing, StateProcessed, StateFailed} {
		if !s.Valid() {
			t.Fatalf("%s should be valid", s)
		}
	}
	if TransferState("BOGUS").Valid() {
		t.Fatal("unknown state must be invalid")
	}
	if !StateProcessed.IsTerminal() || !StateFailed.IsTerminal() {
		t.Fatal("PROCESSED/FAILED are terminal")
	}
	if StatePending.IsTerminal() || StateProcessing.IsTerminal() {
		t.Fatal("PENDING/PROCESSING are not terminal")
	}
}

func TestWallet_CanReceive(t *testing.T) {
	if err := (Wallet{IsActive: true}).CanReceive(); err != nil {
		t.Fatalf("active wallet should receive: %v", err)
	}
	if err := (Wallet{IsActive: false}).CanReceive(); err != ErrWalletInactive {
		t.Fatalf("inactive wallet: got %v, want ErrWalletInactive", err)
	}
}
