package service

import (
	"errors"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/google/uuid"
)

// Ledger-pair shape is service-only; model NewLedgerPair builds a valid pair but
// assertConsistentLedgerPair is the guard before InsertPair.
func TestAssertConsistentLedgerPair(t *testing.T) {
	t.Parallel()

	tr, err := model.NewTransfer("k", "from", "to", 25)
	if err != nil {
		t.Fatal(err)
	}
	if err := tr.MarkProcessed(); err != nil {
		t.Fatal(err)
	}
	debit, credit, err := model.NewLedgerPair(tr)
	if err != nil {
		t.Fatal(err)
	}
	if err := assertConsistentLedgerPair(tr, debit, credit); err != nil {
		t.Fatal(err)
	}

	if err := assertConsistentLedgerPair(tr, nil, credit); !errors.Is(err, model.ErrLedgerRequiresTransfer) {
		t.Fatalf("nil debit: %v", err)
	}

	badType := *debit
	badType.EntryType = model.EntryCredit
	if err := assertConsistentLedgerPair(tr, &badType, credit); !errors.Is(err, model.ErrInvalidEntryType) {
		t.Fatalf("entry type: %v", err)
	}

	otherID := *debit
	otherID.TransferID = uuid.New()
	if err := assertConsistentLedgerPair(tr, &otherID, credit); !errors.Is(err, model.ErrLedgerRequiresTransfer) {
		t.Fatalf("transfer id mismatch: %v", err)
	}

	sameWallet := *credit
	sameWallet.WalletID = debit.WalletID
	if err := assertConsistentLedgerPair(tr, debit, &sameWallet); !errors.Is(err, model.ErrSameWallet) {
		t.Fatalf("same wallet: %v", err)
	}

	unequal := *credit
	unequal.Amount = helpers.Money(1)
	if err := assertConsistentLedgerPair(tr, debit, &unequal); !errors.Is(err, model.ErrInvalidAmount) {
		t.Fatalf("unequal amount: %v", err)
	}

	pending, _ := model.NewTransfer("k2", "from", "to", 25)
	if err := assertConsistentLedgerPair(pending, debit, credit); !errors.Is(err, model.ErrLedgerRequiresTransfer) {
		t.Fatalf("pending transfer: %v", err)
	}
}
