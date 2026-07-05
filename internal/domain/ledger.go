package domain

import (
	"time"

	"github.com/google/uuid"
)

type LedgerEntryType string

const (
	EntryDebit  LedgerEntryType = "DEBIT"
	EntryCredit LedgerEntryType = "CREDIT"
)

type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

func NewLedgerPair(transferID uuid.UUID, fromWalletID, toWalletID string, amount int64) (debit, credit LedgerEntry) {
	now := time.Now().Truncate(time.Microsecond)
	debit = LedgerEntry{
		ID:         uuid.New(),
		TransferID: transferID,
		WalletID:   fromWalletID,
		Type:       EntryDebit,
		Amount:     amount,
		CreatedAt:  now,
	}
	credit = LedgerEntry{
		ID:         uuid.New(),
		TransferID: transferID,
		WalletID:   toWalletID,
		Type:       EntryCredit,
		Amount:     amount,
		CreatedAt:  now,
	}
	return debit, credit
}
