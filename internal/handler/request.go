package handler

import "fmt"

// CreateTransferRequest represents the request body for creating a transfer.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// Validate performs request validation.
func (r *CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return fmt.Errorf("idempotencyKey is required")
	}
	if r.FromWalletID == "" {
		return fmt.Errorf("fromWalletId is required")
	}
	if r.ToWalletID == "" {
		return fmt.Errorf("toWalletId is required")
	}
	if r.Amount <= 0 {
		return fmt.Errorf("amount must be positive")
	}
	if r.FromWalletID == r.ToWalletID {
		return fmt.Errorf("fromWalletId and toWalletId must be different")
	}
	return nil
}

// CreateWalletRequest represents the request body for creating a wallet.
type CreateWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
	Currency       string `json:"currency"`
}

// Validate performs request validation.
func (r *CreateWalletRequest) Validate() error {
	if r.ID == "" {
		return fmt.Errorf("id is required")
	}
	if r.InitialBalance < 0 {
		return fmt.Errorf("initialBalance must be non-negative")
	}
	return nil
}
