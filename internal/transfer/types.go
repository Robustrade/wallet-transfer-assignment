package transfer

import (
	"context"
	"fmt"
)

type CreateRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type Result struct {
	TransferID   string `json:"transferId"`
	State        string `json:"state"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
	FromBalance  int64  `json:"fromBalance"`
	ToBalance    int64  `json:"toBalance"`
}

type APIError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	TransferID string `json:"transferId,omitempty"`
}

type Outcome struct {
	StatusCode int
	Body       []byte
}

type Repository interface {
	Create(context.Context, CreateRequest, [32]byte) (Outcome, error)
}

type Error struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}
