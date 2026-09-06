package domain

import "time"

// Wallet holds a balance in minor currency units (e.g. cents) to avoid
// floating point rounding errors in financial arithmetic.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (w *Wallet) CanDebit(amount int64) bool {
	return w.Balance >= amount
}
