package ledger

import (
	"errors"
	"time"
)

// EntryType classifies the direction of a ledger entry.
type EntryType string

const (
	EntryTypeDebit  EntryType = "DEBIT"
	EntryTypeCredit EntryType = "CREDIT"
)

// Entry is a single row in the double-entry ledger.
//
// Design notes:
//   - No surrogate ID. The natural composite key is (TransferID, Type) —
//     a transfer always produces exactly one DEBIT and one CREDIT, so the pair
//     is uniquely identified without a separate ID column.
//   - Pure domain model — no storage tags, no DB imports.
type Entry struct {
	TransferID string
	WalletID   string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

// Validate checks an entry is internally consistent before persistence.
func (e *Entry) Validate() error {
	if e.TransferID == "" {
		return errors.New("ledger: entry transfer_id is required")
	}
	if e.WalletID == "" {
		return errors.New("ledger: entry wallet_id is required")
	}
	if e.Type != EntryTypeDebit && e.Type != EntryTypeCredit {
		return errors.New("ledger: entry type must be DEBIT or CREDIT")
	}
	if e.Amount <= 0 {
		return errors.New("ledger: entry amount must be positive")
	}
	return nil
}

// Balance holds the current running balance for a wallet.
//
// Design notes:
//   - WalletID is a FK to the wallets table (wallet domain).
//   - Version enables optimistic locking as defence-in-depth alongside
//     the SELECT FOR UPDATE row lock used during transfers.
type Balance struct {
	WalletID  string
	Amount    int64
	UpdatedAt time.Time
}
