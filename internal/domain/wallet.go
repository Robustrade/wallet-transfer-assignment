package domain

import "time"

type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (w *Wallet) HasSufficientFunds(amount int64) bool {
	return w.Balance >= amount
}
