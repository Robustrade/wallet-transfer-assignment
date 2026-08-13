package domain

import "time"

// Wallet holds a balance in the smallest unit of its currency (e.g. cents).
type Wallet struct {
	ID        string
	Balance   int64
	Currency  string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// HasSufficientFunds reports whether the wallet can cover amount.
func (w *Wallet) HasSufficientFunds(amount int64) bool {
	return w.Balance >= amount
}
