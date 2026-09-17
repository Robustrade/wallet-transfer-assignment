// Package domain defines the core entities and business rules.
package domain

import "time"

// Wallet represents a user's wallet with a stored balance.
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}
