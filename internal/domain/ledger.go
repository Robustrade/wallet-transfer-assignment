package domain

import (
	"time"

	"github.com/google/uuid"
)

type LedgerEntryType string

const (
	Debit  LedgerEntryType = "DEBIT"
	Credit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	EntryType  LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}
