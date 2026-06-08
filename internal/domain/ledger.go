package domain

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type LedgerEntryType string

const (
	LedgerEntryTypeDebit  LedgerEntryType = "DEBIT"
	LedgerEntryTypeCredit LedgerEntryType = "CREDIT"
)

var ErrInvalidLedgerEntryType = errors.New("invalid ledger entry type")

type LedgerEntry struct {
	ID         uuid.UUID
	TransferID uuid.UUID
	WalletID   uuid.UUID
	Type       LedgerEntryType
	Amount     int64
	CreatedAt  time.Time
}

func (t LedgerEntryType) IsValid() bool {
	switch t {
	case LedgerEntryTypeDebit, LedgerEntryTypeCredit:
		return true
	default:
		return false
	}
}

func ParseLedgerEntryType(value string) (LedgerEntryType, error) {
	entryType := LedgerEntryType(value)
	if !entryType.IsValid() {
		return "", fmt.Errorf("%w: %s", ErrInvalidLedgerEntryType, value)
	}
	return entryType, nil
}
