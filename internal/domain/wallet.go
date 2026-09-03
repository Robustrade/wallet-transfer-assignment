package domain

import "time"

// Wallet holds a balance in minor currency units (e.g. paise/cents) to
// avoid floating point rounding errors.
type Wallet struct {
	ID        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
