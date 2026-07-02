package domain

import (
	"strings"
	"time"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type TransferResponse struct {
	TransferID     string         `json:"id,omitempty"`
	IdempotencyKey string         `json:"idempotencyKey,omitempty"`
	FromWalletID   string         `json:"fromWalletId,omitempty"`
	ToWalletID     string         `json:"toWalletId,omitempty"`
	Amount         int64          `json:"amount,omitempty"`
	Status         TransferStatus `json:"status,omitempty"`
	Error          string         `json:"error,omitempty"`
}

func (r TransferRequest) Validate() error {

	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return ErrEmptyIdempotency
	}

	if strings.TrimSpace(r.FromWalletID) == "" ||
		strings.TrimSpace((r.ToWalletID)) == "" {
		return ErrEmptyWalletID
	}

	if strings.TrimSpace(r.FromWalletID) == strings.TrimSpace((r.ToWalletID)) {
		return ErrSameWallet
	}

	if r.Amount <= 0 {
		return ErrInvalidAmount
	}

	return nil
}
