package dtos

import "time"

type TransferRequest struct {
	IdempotencyKey string  `json:"idempotencyKey" validate:"required,min=3,max=32"`
	FromWalletId   string  `json:"fromWalletId" validate:"required"`
	ToWalletId     string  `json:"toWalletId" validate:"required"`
	Amount         float64 `json:"amount" validate:"required,gt=0"`
}

type ErrorResponse struct {
	Error   string   `json:"error"`
	Details []string `json:"details,omitempty"`
}

type TransferResponse struct {
	TransactionId  string    `json:"transactionId"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletId   string    `json:"fromWalletId"`
	ToWalletId     string    `json:"toWalletId"`
	Amount         float64   `json:"amount"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"createdAt"`
}
