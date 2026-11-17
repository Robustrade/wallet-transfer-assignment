package domain

import "time"

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type Transfer struct {
	ID           string         `json:"id"`
	FromWalletID string         `json:"fromWalletId"`
	ToWalletID   string         `json:"toWalletId"`
	Amount       int64          `json:"amount"`
	Status       TransferStatus `json:"status"`
	CreatedAt    time.Time      `json:"createdAt"`
}
