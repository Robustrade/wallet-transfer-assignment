package model

import (
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/google/uuid"
)

// EntryType is the direction of a ledger row. Amounts stay positive.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is one side of a double-entry pair for a transfer.
type LedgerEntry struct {
	ID         int64
	WalletID   string
	TransferID uuid.UUID
	EntryType  EntryType
	Amount     helpers.Money
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// NewLedgerPair builds the debit (from) and credit (to) rows for a PROCESSED transfer.
// Call after MarkProcessed so the pair always matches a successful movement.
func NewLedgerPair(t *Transfer) (debit, credit *LedgerEntry, err error) {
	if t == nil {
		return nil, nil, ErrLedgerRequiresTransfer
	}
	if t.Status != StatusProcessed {
		return nil, nil, ErrLedgerRequiresTransfer
	}
	now := time.Now().UTC()
	debit = &LedgerEntry{
		WalletID:   t.FromWalletID,
		TransferID: t.ID,
		EntryType:  EntryDebit,
		Amount:     t.Amount,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	credit = &LedgerEntry{
		WalletID:   t.ToWalletID,
		TransferID: t.ID,
		EntryType:  EntryCredit,
		Amount:     t.Amount,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	return debit, credit, nil
}
