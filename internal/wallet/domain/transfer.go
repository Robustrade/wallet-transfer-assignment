package domain

import (
	"errors"
	"strings"
)

const (
	StatusPending   = "PENDING"
	StatusProcessed = "PROCESSED"
	StatusFailed    = "FAILED"
)

type Transfer struct {
	ID             string
	IdempotencyKey string `json:"idempotencyKey"`
	FromWallet     string `json:"fromWalletId"`
	ToWallet       string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
}

type Wallet struct {
	ID      string
	Balance int64
}

func (tr *Transfer) Validate() error {
	if strings.TrimSpace(tr.IdempotencyKey) == "" {
		return errors.New("idempotency key required")
	}
	if strings.TrimSpace(tr.FromWallet) == "" {
		return errors.New("Source wallet id required")
	}
	if strings.TrimSpace(tr.ToWallet) == "" {
		return errors.New("Source wallet id required")
	}
	if strings.TrimSpace(tr.FromWallet) == strings.TrimSpace(tr.ToWallet) {
		return errors.New("Source and Destination wallets must have different id")
	}
	if tr.Amount <= 0 {
		return errors.New("Amount should be greater than zero")
	}

	return nil
}
