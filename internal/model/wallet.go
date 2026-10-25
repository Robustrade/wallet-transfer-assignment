package model

import (
	"strings"
	"time"
)

// Wallet represents a financial wallet holding funds in the smallest currency denomination (e.g. cents/paise).
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"` // Stored in cents/paise (integer representation)
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Validate checks whether a wallet instance adheres to domain constraints.
func (w *Wallet) Validate() error {
	if strings.TrimSpace(w.ID) == "" {
		return ErrWalletNotFound
	}
	if w.Balance < 0 {
		return ErrInsufficientFunds
	}
	if strings.TrimSpace(w.Currency) == "" {
		w.Currency = "USD"
	}
	return nil
}
