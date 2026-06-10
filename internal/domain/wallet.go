package domain

import "time"

// Wallet represents a user's wallet with a balance denominated in a currency.
type Wallet struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// CreateWalletRequest represents the payload for creating a new wallet.
type CreateWalletRequest struct {
	ID       string `json:"id"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

// Validate checks that the wallet creation request is well-formed.
func (r *CreateWalletRequest) Validate() error {
	if r.ID == "" {
		return NewInvalidInputError("wallet id is required")
	}
	if r.Balance < 0 {
		return NewInvalidInputError("initial balance cannot be negative")
	}
	if r.Currency == "" {
		r.Currency = "USD"
	}
	if len(r.Currency) != 3 {
		return NewInvalidInputError("currency must be a 3-letter ISO code")
	}
	return nil
}
