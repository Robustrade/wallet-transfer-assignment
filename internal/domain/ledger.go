package domain

import "github.com/google/uuid"

type EntryType string

const (
	Debit  EntryType = "DEBIT"
	Credit EntryType = "CREDIT"
)

type LedgerEntry struct {
	ID uuid.UUID

	TransferID uuid.UUID

	WalletID uuid.UUID

	Type EntryType

	Amount int64
}
