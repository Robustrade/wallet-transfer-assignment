package domain

import "time"

// Wallet represents a stored wallet balance. Balances are integer minor
// units (e.g. cents) to avoid floating point rounding issues.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
