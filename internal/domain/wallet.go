package domain

import "time"

// Wallet holds a stored balance in minor units. The balance is the operational
// value; the ledger is the audit record, and the two must always agree.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CanCover reports whether this wallet holds enough to fund the amount.
//
// The answer is only meaningful while the wallet's row is locked: read without
// a lock, it is a statement about a balance that may already have changed.
func (w Wallet) CanCover(amount int64) bool {
	return w.Balance >= amount
}
