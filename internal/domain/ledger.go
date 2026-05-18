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
	ID         string
	WalletID   string
	TransferID string
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

func NewDebitEntry(walletID, transferID string, amount int64) (*LedgerEntry, error) {
	return newLedgerEntry(walletID, transferID, LedgerEntryTypeDebit, amount)
}

func NewCreditEntry(walletID, transferID string, amount int64) (*LedgerEntry, error) {
	return newLedgerEntry(walletID, transferID, LedgerEntryTypeCredit, amount)
}

func newLedgerEntry(
	walletID string,
	transferID string,
	entryType LedgerEntryType,
	amount int64,
) (*LedgerEntry, error) {
	if walletID == "" {
		return nil, ErrInvalidWalletID
	}

	if transferID == "" {
		return nil, ErrInvalidTransferID
	}

	if amount <= 0 {
		return nil, ErrInvalidAmount
	}

	if entryType != LedgerEntryTypeDebit && entryType != LedgerEntryTypeCredit {
		return nil, ErrInvalidLedgerEntryType
	}

	return &LedgerEntry{
		ID:         uuid.NewString(),
		WalletID:   walletID,
		TransferID: transferID,
		Type:       entryType,
		Amount:     amount,
	}, nil
}
