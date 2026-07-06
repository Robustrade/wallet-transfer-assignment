package domain

import (
	"time"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	// FromWalletId   string `json:"fromWalletId" binding:"required,uuid"`
	// ToWalletId     string `json:"toWalletId" binding:"required,uuid"`
	FromWalletId string  `json:"fromWalletId" binding:"required"`
	ToWalletId   string  `json:"toWalletId" binding:"required"`
	Amount       float64 `json:"amount" binding:"required,gt=0"`
}

type TransferResponse struct {
	TransferId     string         `json:"transferId"`
	IdempotencyKey string         `json:"idempotencyKey"`
	FromWalletId   string         `json:"fromWalletId"`
	ToWalletId     string         `json:"toWalletId"`
	Amount         float64        `json:"amount"`
	Status         TransferStatus `json:"status"`
	CreatedAt      time.Time      `json:"createdAt"`
}

type WalletResponse struct {
	WalletId string  `json:"walletId"`
	Balance  float64 `json:"balance"`
}
