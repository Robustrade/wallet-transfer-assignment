package domain

import "time"

// EntryType is one side of a double-entry posting.
type EntryType string

const (
	EntryDebit  EntryType = "DEBIT"
	EntryCredit EntryType = "CREDIT"
)

// LedgerEntry is an immutable posting against one wallet for one transfer.
// Amount is always positive. Direction is EntryType.
type LedgerEntry struct {
	ID         string
	WalletID   string
	TransferID string
	Type       EntryType
	Amount     int64
	CreatedAt  time.Time
}

// LedgerPair builds the debit and credit rows for a pending or processed transfer.
// Both amounts equal the transfer amount. The debit hits the source wallet and
// the credit hits the destination wallet.
func LedgerPair(transfer Transfer, debitID, creditID string, now time.Time) (LedgerEntry, LedgerEntry, error) {
	if debitID == "" || creditID == "" || debitID == creditID {
		return LedgerEntry{}, LedgerEntry{}, &ValidationError{Details: []FieldError{{
			Field:   "id",
			Message: "debit and credit entry ids must be distinct and non-empty",
		}}}
	}
	if transfer.ID == "" || transfer.Amount <= 0 || transfer.FromWalletID == "" || transfer.ToWalletID == "" {
		return LedgerEntry{}, LedgerEntry{}, ErrInvariant
	}
	if transfer.Status == StatusFailed {
		return LedgerEntry{}, LedgerEntry{}, ErrInvalidTransition
	}
	now = now.UTC()
	debit := LedgerEntry{
		ID:         debitID,
		WalletID:   transfer.FromWalletID,
		TransferID: transfer.ID,
		Type:       EntryDebit,
		Amount:     transfer.Amount,
		CreatedAt:  now,
	}
	credit := LedgerEntry{
		ID:         creditID,
		WalletID:   transfer.ToWalletID,
		TransferID: transfer.ID,
		Type:       EntryCredit,
		Amount:     transfer.Amount,
		CreatedAt:  now,
	}
	return debit, credit, nil
}
