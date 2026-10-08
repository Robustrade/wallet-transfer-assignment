package service

import (
	"context"
	"testing"

	"wallet-transfer-assignment/internal/domain"
)

// assertBalancedTransferLedger verifies the whole transfer-level invariant,
// independent of row order. Comparing the two positive amounts avoids overflow
// from accumulating multiple int64 amounts before subtracting debits.
func assertBalancedTransferLedger(t *testing.T, transfer domain.Transfer, entries []domain.LedgerEntry) {
	t.Helper()
	if transfer.Status() != domain.TransferStatusProcessed {
		t.Fatalf("transfer %s is not PROCESSED", transfer.ID)
	}
	if len(entries) != 2 {
		t.Fatalf("transfer %s has %d entries, want exactly 2", transfer.ID, len(entries))
	}
	var debit, credit *domain.LedgerEntry
	for i := range entries {
		entry := &entries[i]
		if entry.TransferID != transfer.ID || entry.Amount != transfer.Amount || entry.Amount <= 0 {
			t.Fatalf("ledger entry does not match transfer: %+v", entry)
		}
		switch entry.Type {
		case domain.LedgerEntryDebit:
			if debit != nil || entry.WalletID != transfer.FromWalletID {
				t.Fatalf("duplicate or incorrect debit: %+v", entry)
			}
			debit = entry
		case domain.LedgerEntryCredit:
			if credit != nil || entry.WalletID != transfer.ToWalletID {
				t.Fatalf("duplicate or incorrect credit: %+v", entry)
			}
			credit = entry
		default:
			t.Fatalf("unexpected entry type: %s", entry.Type)
		}
	}
	if debit == nil || credit == nil {
		t.Fatal("expected one DEBIT and one CREDIT")
	}
	if debit.Amount != credit.Amount {
		t.Fatalf("unbalanced ledger: debit %d credit %d", debit.Amount, credit.Amount)
	}
	if debit.ID == credit.ID {
		t.Fatal("ledger entries must have distinct IDs")
	}
}

func TestTransferLedgerBalancedInBothDirections(t *testing.T) {
	for _, req := range []Request{{"forward", "a", "b", 10}, {"reverse", "b", "a", 10}} {
		t.Run(req.IdempotencyKey, func(t *testing.T) {
			m := fixture()
			s := testService(m)
			original, err := s.Transfer(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			for _, transfer := range m.transfers {
				assertBalancedTransferLedger(t, transfer, m.ledger)
			}
			// Replay cannot append either ledger leg or mutate balances again.
			replay, err := s.Transfer(context.Background(), req)
			if err != nil || replay != original {
				t.Fatalf("replay: %v", err)
			}
			if len(m.transfers) != 1 {
				t.Fatal("retry added transfer")
			}
			for _, transfer := range m.transfers {
				assertBalancedTransferLedger(t, transfer, m.ledger)
			}
			if req.FromWalletID == "a" {
				if m.wallets["a"].Balance != 90 || m.wallets["b"].Balance != 30 {
					t.Fatal("incorrect forward balances")
				}
			} else {
				if m.wallets["a"].Balance != 110 || m.wallets["b"].Balance != 10 {
					t.Fatal("incorrect reverse balances")
				}
			}
		})
	}
}
