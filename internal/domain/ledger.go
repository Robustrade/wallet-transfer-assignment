package domain

import (
	"time"

	"github.com/google/uuid"
)

type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	EntryType  EntryType
	Amount     Amount
	CreatedAt  time.Time
}
