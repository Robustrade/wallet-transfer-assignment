package domain

import (
	"time"

	"github.com/google/uuid"
)

type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}
