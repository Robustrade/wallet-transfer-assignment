package domain

import (
	"time"

	"github.com/google/uuid"
)

// LedgerEntry is one side (debit or credit) of a double-entry ledger record.
// Every processed transfer produces exactly two of these, sharing TransferID.
type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	EntryType  EntryType
	Amount     int64 // minor units (cents); always positive
	CreatedAt  time.Time
}
