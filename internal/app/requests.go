package app

import "errors"

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (r *CreateTransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return errors.New("idempotencyKey is required")
	}
	if r.FromWalletID == "" {
		return errors.New("fromWalletId is required")
	}
	if r.ToWalletID == "" {
		return errors.New("toWalletId is required")
	}
	if r.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	return nil
}

type CreateWalletRequest struct {
	ID      string `json:"id,omitempty"`
	Balance int64  `json:"balance"`
}

func (r *CreateWalletRequest) Validate() error {
	if r.Balance < 0 {
		return errors.New("balance must be zero or greater")
	}
	return nil
}
