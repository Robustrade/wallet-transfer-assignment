package domain

import "time"

type Wallet struct {
	Id        string
	Balance   int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
