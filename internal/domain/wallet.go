package domain

import "time"

// Wallet holds a stored balance in exact minor units.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
