package domain

import (
	"time"

	"github.com/google/uuid"
)

type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         int64
	TransferID uuid.UUID
	WalletID   string
	Type       EntryType
	Amount     Amount
	Currency   string
	CreatedAt  time.Time
}

func LedgerEntriesFor(t *Transfer) [2]LedgerEntry {
	return [2]LedgerEntry{
		{
			TransferID: t.ID,
			WalletID:   t.FromWalletID,
			Type:       EntryTypeDebit,
			Amount:     t.Amount,
			Currency:   t.Currency,
		},
		{
			TransferID: t.ID,
			WalletID:   t.ToWalletID,
			Type:       EntryTypeCredit,
			Amount:     t.Amount,
			Currency:   t.Currency,
		},
	}
}
