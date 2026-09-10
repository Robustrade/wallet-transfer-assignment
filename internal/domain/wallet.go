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

// HasSufficientFunds checks if the wallet has enough balance for the given amount.
func (w *Wallet) HasSufficientFunds(amount int64) bool {
	return w.Balance >= amount
}

// Debit reduces the wallet balance by the given amount.
// Returns false if insufficient funds.
func (w *Wallet) Debit(amount int64) bool {
	if !w.HasSufficientFunds(amount) {
		return false
	}
	w.Balance -= amount
	w.UpdatedAt = time.Now()
	return true
}

// Credit increases the wallet balance by the given amount.
func (w *Wallet) Credit(amount int64) {
	w.Balance += amount
	w.UpdatedAt = time.Now()
}
