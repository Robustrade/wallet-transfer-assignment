package repository_test

import (
	"context"
	"errors"
	"testing"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/repository"
)

// These inputs are rejected before any SQL runs, so a nil transaction handle is
// enough. Behaviour that needs PostgreSQL is covered by the integration tests.

func entry(typ domain.LedgerEntryType, walletID, transferID, amount int64) domain.LedgerEntry {
	return domain.LedgerEntry{TransferID: transferID, WalletID: walletID, EntryType: typ, Amount: amount}
}

func TestLedgerRepository_CreateEntriesRejectsInvalidPairs(t *testing.T) {
	debit := entry(domain.LedgerDebit, 1, 10, 100)
	credit := entry(domain.LedgerCredit, 2, 10, 100)

	tests := []struct {
		name    string
		entries []domain.LedgerEntry
		wantErr error
	}{
		{"no entries", nil, domain.ErrInvalidLedgerEntry},
		{"single entry", []domain.LedgerEntry{debit}, domain.ErrInvalidLedgerEntry},
		{"three entries", []domain.LedgerEntry{debit, credit, credit}, domain.ErrInvalidLedgerEntry},
		{"credit before debit", []domain.LedgerEntry{credit, debit}, domain.ErrInvalidLedgerEntry},
		{"two debits", []domain.LedgerEntry{debit, debit}, domain.ErrInvalidLedgerEntry},
		{"different transfers", []domain.LedgerEntry{debit, entry(domain.LedgerCredit, 2, 11, 100)}, domain.ErrInvalidLedgerEntry},
		{"same wallet", []domain.LedgerEntry{debit, entry(domain.LedgerCredit, 1, 10, 100)}, domain.ErrInvalidLedgerEntry},
		{"unbalanced amounts", []domain.LedgerEntry{debit, entry(domain.LedgerCredit, 2, 10, 99)}, domain.ErrInvalidLedgerEntry},
		{"zero amounts", []domain.LedgerEntry{entry(domain.LedgerDebit, 1, 10, 0), entry(domain.LedgerCredit, 2, 10, 0)}, domain.ErrInvalidAmount},
		{"missing transfer id", []domain.LedgerEntry{entry(domain.LedgerDebit, 1, 0, 5), entry(domain.LedgerCredit, 2, 0, 5)}, domain.ErrInvalidLedgerEntry},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := repository.NewLedgerRepository().CreateEntries(context.Background(), nil, tc.entries)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestIdempotencyRepository_SaveResponseRequiresStatusAndBody(t *testing.T) {
	status := 201

	tests := map[string]*domain.IdempotencyRecord{
		"missing status": {ID: 1, ResponseBody: []byte(`{}`)},
		"empty body":     {ID: 1, ResponseStatus: &status},
	}
	for name, rec := range tests {
		t.Run(name, func(t *testing.T) {
			err := repository.NewIdempotencyRepository().SaveResponse(context.Background(), nil, rec)
			if !errors.Is(err, domain.ErrInvalidIdempotency) {
				t.Fatalf("got %v, want ErrInvalidIdempotency", err)
			}
		})
	}
}

func TestTransferRepository_UpdateStatusOnlyAcceptsTerminalStatuses(t *testing.T) {
	for _, status := range []domain.TransferStatus{domain.TransferPending, "BOGUS", ""} {
		t.Run(string(status), func(t *testing.T) {
			err := repository.NewTransferRepository().UpdateStatus(context.Background(), nil, &domain.Transfer{ID: 1, Status: status})
			if !errors.Is(err, domain.ErrInvalidTransition) {
				t.Fatalf("got %v, want ErrInvalidTransition", err)
			}
		})
	}
}
