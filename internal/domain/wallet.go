package domain

import "time"

// Wallet holds a balance in minor currency units.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CanDebit reports whether the wallet holds at least amount.
func (w Wallet) CanDebit(amount int64) bool {
	return w.Balance >= amount
}
