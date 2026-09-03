package domain

import "time"

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         string
	TransferID string
	WalletID   string
	EntryType  EntryType
	Amount     int64
	CreatedAt  time.Time
}

func NewLedgerPair(t *Transfer, now time.Time) (debit, credit LedgerEntry) {
	debit = LedgerEntry{
		ID:         NewID("le"),
		TransferID: t.ID,
		WalletID:   t.FromWalletID,
		EntryType:  EntryDebit,
		Amount:     t.Amount,
		CreatedAt:  now,
	}
	credit = LedgerEntry{
		ID:         NewID("le"),
		TransferID: t.ID,
		WalletID:   t.ToWalletID,
		EntryType:  EntryCredit,
		Amount:     t.Amount,
		CreatedAt:  now,
	}
	return debit, credit
}
