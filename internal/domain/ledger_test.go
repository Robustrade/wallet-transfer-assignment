package domain_test

import (
	"testing"

	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
)

func TestLedgerEntriesDebitTheSourceAndCreditTheDestination(t *testing.T) {
	transfer := domain.Transfer{
		ID:           "transfer_1",
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       100,
	}

	entries := domain.LedgerEntriesFor(transfer)

	if len(entries) != 2 {
		t.Fatalf("got %d entries, want exactly 2", len(entries))
	}

	debit, credit := entries[0], entries[1]

	if debit.Type != domain.EntryDebit || debit.WalletID != "wallet_1" {
		t.Errorf("first entry is %s on %s, want DEBIT on wallet_1", debit.Type, debit.WalletID)
	}
	if credit.Type != domain.EntryCredit || credit.WalletID != "wallet_2" {
		t.Errorf("second entry is %s on %s, want CREDIT on wallet_2", credit.Type, credit.WalletID)
	}
	for _, entry := range entries {
		if entry.TransferID != "transfer_1" {
			t.Errorf("entry belongs to transfer %s, want transfer_1", entry.TransferID)
		}
	}
}

// The invariant the ledger exists to guarantee: the two sides cancel out, so
// summing every entry in the system yields zero.
func TestLedgerEntriesBalance(t *testing.T) {
	entries := domain.LedgerEntriesFor(domain.Transfer{
		ID:           "transfer_1",
		FromWalletID: "wallet_1",
		ToWalletID:   "wallet_2",
		Amount:       100,
	})

	var net int64
	for _, entry := range entries {
		switch entry.Type {
		case domain.EntryDebit:
			net -= entry.Amount
		case domain.EntryCredit:
			net += entry.Amount
		default:
			t.Fatalf("unexpected entry type %q", entry.Type)
		}
	}

	if net != 0 {
		t.Fatalf("entries net to %d, want 0", net)
	}
}
