package model_test

import (
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

func TestNewLedgerPair_RequiresProcessed(t *testing.T) {
	t.Parallel()
	tr, _ := model.NewTransfer("k", "from_w", "to_w", 25)
	if _, _, err := model.NewLedgerPair(tr); err != model.ErrLedgerRequiresTransfer {
		t.Fatalf("pending: %v", err)
	}
	_ = tr.MarkProcessed()
	debit, credit, err := model.NewLedgerPair(tr)
	if err != nil {
		t.Fatal(err)
	}
	if debit.EntryType != model.EntryDebit || debit.WalletID != "from_w" || debit.Amount != 25 {
		t.Fatalf("debit=%+v", debit)
	}
	if credit.EntryType != model.EntryCredit || credit.WalletID != "to_w" || credit.Amount != 25 {
		t.Fatalf("credit=%+v", credit)
	}
	if debit.TransferID != tr.ID || credit.TransferID != tr.ID {
		t.Fatal("transfer id mismatch")
	}
}

func TestNewLedgerPair_NilTransfer(t *testing.T) {
	t.Parallel()
	if _, _, err := model.NewLedgerPair(nil); err != model.ErrLedgerRequiresTransfer {
		t.Fatalf("got %v", err)
	}
}
