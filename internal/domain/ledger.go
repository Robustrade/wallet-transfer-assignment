package domain

import (
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// EntryType is the direction of a ledger entry. Both legs store a positive
// amount; direction is carried by the type.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

func (t EntryType) Valid() bool {
	return t == EntryDebit || t == EntryCredit
}

// LedgerEntry is one immutable leg of a double-entry record.
type LedgerEntry struct {
	ID         uuid.UUID
	WalletID   uuid.UUID
	TransferID uuid.UUID
	Type       EntryType
	Amount     decimal.Decimal
	CreatedAt  time.Time
}
