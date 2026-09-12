package service

import (
	"time"

	"wallet-transfer-assignment/internal/domain"
)

// CreateTransferRequest encapsulates client input for initiating a transfer.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// TransferResponse is the standardized response returned to API consumers.
type TransferResponse struct {
	TransferID     string                `json:"transferId"`
	IdempotencyKey string                `json:"idempotencyKey"`
	FromWalletID   string                `json:"fromWalletId"`
	ToWalletID     string                `json:"toWalletId"`
	Amount         int64                 `json:"amount"`
	Status         domain.TransferStatus `json:"status"`
	FailureReason  *string               `json:"failureReason,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	IsReplay       bool                  `json:"isReplay,omitempty"`
	ResponseCode   int                   `json:"-"`
}

// CreateWalletRequest encapsulates parameters for seeding a new wallet.
type CreateWalletRequest struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name"`
	InitialBalance int64  `json:"initialBalance"`
	Currency       string `json:"currency,omitempty"`
}
