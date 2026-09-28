package domain

import (
	"time"

	"github.com/google/uuid"
)

// EntryType is the side of a double-entry ledger line.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one immutable line in the ledger. Amount is always positive;
// the direction comes from Type.
type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

// NewLedgerEntries builds the two lines for a transfer: a DEBIT on the source
// wallet and a CREDIT on the destination, both for the full amount. Building
// them as a pair means callers cannot create one without the other, and the
// ledger always balances. The database backs this up with
// UNIQUE(transfer_id, entry_type).
func NewLedgerEntries(t *Transfer) (debit, credit LedgerEntry) {
	debit = LedgerEntry{
		ID:         uuid.Must(uuid.NewV7()),
		TransferID: t.ID,
		WalletID:   t.FromWalletID,
		Type:       EntryDebit,
		Amount:     t.Amount,
		CreatedAt:  t.CreatedAt,
	}
	credit = LedgerEntry{
		ID:         uuid.Must(uuid.NewV7()),
		TransferID: t.ID,
		WalletID:   t.ToWalletID,
		Type:       EntryCredit,
		Amount:     t.Amount,
		CreatedAt:  t.CreatedAt,
	}
	return debit, credit
}
