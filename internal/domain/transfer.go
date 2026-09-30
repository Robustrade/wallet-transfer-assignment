package domain

import "errors"

var (
	ErrInvalidRequest      = errors.New("invalid request")
	ErrIdempotencyConflict = errors.New("idempotency key reused with a different request")
	ErrWalletNotFound      = errors.New("wallet not found")
)

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type Transfer struct {
	ID           string `json:"id"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status"`
}

type TransferResult struct {
	Transfer   Transfer `json:"transfer"`
	HTTPStatus int      `json:"-"`
}

type Wallet struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

func (r TransferRequest) Validate() error {
	if r.IdempotencyKey == "" || r.FromWalletID == "" || r.ToWalletID == "" || r.Amount <= 0 || r.FromWalletID == r.ToWalletID {
		return ErrInvalidRequest
	}
	return nil
}
