package domain

import "time"

type Wallet struct {
	ID        string
	OwnerName string
	Balance   float64
	CreatedAt time.Time
	UpdatedAt time.Time
}
