package domain

import "time"

// Wallet represents a user's wallet with a balance.
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
