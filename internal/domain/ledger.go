package domain

import (
	"time"

	"github.com/google/uuid"
)

type LedgerType string

const (
	LedgerTypeDebit  LedgerType = "DEBIT"
	LedgerTypeCredit LedgerType = "CREDIT"
)

type LedgerEntry struct {
	ID         uuid.UUID  `json:"entryId"`
	TransferID uuid.UUID  `json:"transferId"`
	WalletID   uuid.UUID  `json:"walletId"`
	Type       LedgerType `json:"type"`
	Amount     Money      `json:"amount"`
	CreatedAt  time.Time  `json:"createdAt"`
}

func NewLedgerEntry(id, transferID, walletID uuid.UUID, entryType LedgerType, amount Money) (*LedgerEntry, error) {
	if entryType != LedgerTypeDebit && entryType != LedgerTypeCredit {
		return nil, ErrInvalidInput("ledger entry type must be DEBIT or CREDIT")
	}
	if !amount.IsPositive() {
		return nil, ErrInvalidAmount("ledger entry amount must be greater than zero")
	}
	return &LedgerEntry{
		ID:         id,
		TransferID: transferID,
		WalletID:   walletID,
		Type:       entryType,
		Amount:     amount,
		CreatedAt:  time.Time{}.UTC(),
	}, nil
}
