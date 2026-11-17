
package domain

import "time"

type Wallet struct {
	ID        string    `json:"id"`
	Balance   float64   `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
}
