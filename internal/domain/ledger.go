package domain

import (
	"time"

	"github.com/google/uuid"
)

// LedgerEntry records one side of a double-entry bookkeeping transaction.
// Every transfer produces exactly two entries: a debit from the source wallet
// and a credit to the destination wallet.
type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	Type       EntryType
	Amount     Amount
	CreatedAt  time.Time
}

// NewLedgerEntry constructs a ledger entry with a newly generated UUID v7 ID.
func NewLedgerEntry(transferID, walletID uuid.UUID, typ EntryType, amount Amount) LedgerEntry {
	return LedgerEntry{
		ID:         NewID(),
		TransferID: transferID,
		WalletID:   walletID,
		Type:       typ,
		Amount:     amount,
		CreatedAt:  time.Now().UTC(),
	}
}
