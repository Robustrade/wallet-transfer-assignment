package domain

import "time"

type Wallet struct {
	ID        string
	Balance   int64 // minor units (e.g. cents) — integer, never floating point
	CreatedAt time.Time
	UpdatedAt time.Time
}
