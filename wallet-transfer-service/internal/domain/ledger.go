package domain

import "github.com/google/uuid"

type LedgerEntryType string

const (
	LedgerDebit  LedgerEntryType = "DEBIT"
	LedgerCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	Type       LedgerEntryType
	Amount     int64
}
