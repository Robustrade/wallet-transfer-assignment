package domain

import "time"

// Wallet holds a user's balance for a given currency.
type Wallet struct {
	ID        string
	UserID    string
	Balance   int64
	Currency  string
	CreatedAt time.Time
}
