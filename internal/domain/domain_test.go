package domain

import "testing"

func TestNewWalletValidation(t *testing.T) {
	_, err := NewWallet("", 100)
	if err == nil {
		t.Fatal("expected error for empty wallet ID")
	}

	_, err = NewWallet("w1", -1)
	if err == nil {
		t.Fatal("expected error for negative wallet balance")
	}

	wallet, err := NewWallet("w1", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wallet.ID != "w1" {
		t.Fatalf("expected wallet ID w1, got %s", wallet.ID)
	}
	if wallet.Balance != 100 {
		t.Fatalf("expected balance 100, got %d", wallet.Balance)
	}
}

func TestWalletDebitCredit(t *testing.T) {
	wallet, err := NewWallet("w1", 100)
	if err != nil {
		t.Fatal(err)
	}

	if err := wallet.Debit(100); err != nil {
		t.Fatalf("unexpected debit error: %v", err)
	}
	if wallet.Balance != 0 {
		t.Fatalf("expected balance 0 after debit, got %d", wallet.Balance)
	}

	err = wallet.Debit(1)
	if err == nil {
		t.Fatal("expected insufficient funds error")
	}

	err = wallet.Credit(50)
	if err != nil {
		t.Fatalf("unexpected credit error: %v", err)
	}
	if wallet.Balance != 50 {
		t.Fatalf("expected balance 50 after credit, got %d", wallet.Balance)
	}

	err = wallet.Credit(0)
	if err == nil {
		t.Fatal("expected invalid amount error for credit 0")
	}
}

func TestTransferCreationValidation(t *testing.T) {
	_, err := NewTransfer("t1", "", "w1", "w2", 100)
	if err == nil {
		t.Fatal("expected error for missing idempotency key")
	}

	_, err = NewTransfer("t1", "k1", "w1", "w1", 100)
	if err == nil {
		t.Fatal("expected error for same wallet IDs")
	}

	_, err = NewTransfer("t1", "k1", "", "w2", 100)
	if err == nil {
		t.Fatal("expected error for empty from wallet ID")
	}

	transfer, err := NewTransfer("t1", "k1", "w1", "w2", 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer.Status != TransferStatusPending {
		t.Fatalf("expected status PENDING, got %s", transfer.Status)
	}
}

func TestTransferStateTransitions(t *testing.T) {
	transfer, err := NewTransfer("t1", "k1", "w1", "w2", 100)
	if err != nil {
		t.Fatal(err)
	}

	if err := transfer.TransitionTo(TransferStatusProcessed, ""); err != nil {
		t.Fatalf("expected transition to PROCESSED, got %v", err)
	}
	if transfer.Status != TransferStatusProcessed {
		t.Fatalf("expected status PROCESSED, got %s", transfer.Status)
	}

	err = transfer.TransitionTo(TransferStatusFailed, "failure")
	if err == nil {
		t.Fatal("expected error transitioning from PROCESSED to FAILED")
	}

	transfer, err = NewTransfer("t2", "k2", "w1", "w2", 100)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfer.TransitionTo(TransferStatusFailed, "insufficient funds"); err != nil {
		t.Fatalf("expected transition to FAILED, got %v", err)
	}
	if transfer.Status != TransferStatusFailed {
		t.Fatalf("expected status FAILED, got %s", transfer.Status)
	}
	if transfer.FailureReason != "insufficient funds" {
		t.Fatalf("expected failure reason preserved, got %s", transfer.FailureReason)
	}
}

func TestLedgerEntryValidation(t *testing.T) {
	_, err := NewLedgerEntry("e1", "t1", "w1", LedgerEntryType("INVALID"), 100)
	if err == nil {
		t.Fatal("expected error for invalid ledger entry type")
	}

	_, err = NewLedgerEntry("e1", "t1", "w1", LedgerEntryTypeDebit, 0)
	if err == nil {
		t.Fatal("expected error for non-positive ledger amount")
	}

	entry, err := NewLedgerEntry("e1", "t1", "w1", LedgerEntryTypeDebit, 100)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if entry.EntryType != LedgerEntryTypeDebit {
		t.Fatalf("expected entry type DEBIT, got %s", entry.EntryType)
	}
}

func TestValidateLedgerEntries(t *testing.T) {
	entries := []LedgerEntry{
		{ID: "le1", TransferID: "t1", WalletID: "w1", EntryType: LedgerEntryTypeDebit, Amount: 100},
		{ID: "le2", TransferID: "t1", WalletID: "w2", EntryType: LedgerEntryTypeCredit, Amount: 100},
	}

	if err := ValidateLedgerEntries(entries); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}

	invalidEntries := []LedgerEntry{
		{ID: "le1", TransferID: "t1", WalletID: "w1", EntryType: LedgerEntryTypeDebit, Amount: 100},
		{ID: "le2", TransferID: "t1", WalletID: "w2", EntryType: LedgerEntryTypeDebit, Amount: 100},
	}
	if err := ValidateLedgerEntries(invalidEntries); err == nil {
		t.Fatal("expected error for duplicate entry types")
	}

	mismatchedAmount := []LedgerEntry{
		{ID: "le1", TransferID: "t1", WalletID: "w1", EntryType: LedgerEntryTypeDebit, Amount: 100},
		{ID: "le2", TransferID: "t1", WalletID: "w2", EntryType: LedgerEntryTypeCredit, Amount: 50},
	}
	if err := ValidateLedgerEntries(mismatchedAmount); err == nil {
		t.Fatal("expected error for mismatched ledger amounts")
	}

	sameWallet := []LedgerEntry{
		{ID: "le1", TransferID: "t1", WalletID: "w1", EntryType: LedgerEntryTypeDebit, Amount: 100},
		{ID: "le2", TransferID: "t1", WalletID: "w1", EntryType: LedgerEntryTypeCredit, Amount: 100},
	}
	if err := ValidateLedgerEntries(sameWallet); err == nil {
		t.Fatal("expected error for entries using same wallet")
	}
}
