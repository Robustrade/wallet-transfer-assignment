package domain

import "time"

// Wallet represents a user's wallet and its current balance.
// Balance is stored in the smallest currency unit (e.g. cents) to avoid
// floating-point arithmetic entirely.
type Wallet struct {
	ID        string
	Balance   int64
	Version   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
