package domain

import "time"

// Wallet holds the current balance for a single account.
//
// Balance is expressed in the smallest currency unit (e.g. cents) as an
// int64 to avoid floating point rounding errors in financial calculations.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CanDebit reports whether amount can be safely subtracted from the wallet
// balance without going negative.
func (w Wallet) CanDebit(amount int64) bool {
	return amount > 0 && w.Balance >= amount
}
