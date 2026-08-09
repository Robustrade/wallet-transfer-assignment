package model

import (
	"errors"
	"strings"

	db "github.com/chaitanya-bhagat/wallet-transfer-assignment/database/generated"
)

var (
	ErrInvalidIdempotencyKey    = errors.New("idempotency key is required")
	ErrInvalidSourceWallet      = errors.New("source wallet is required")
	ErrInvalidDestinationWallet = errors.New("destination wallet is required")
	ErrSameWallet               = errors.New("source and destination wallet must be different")
	ErrInvalidAmount            = errors.New("amount must be greater than zero")
	ErrInvalidStatus            = errors.New("status must be one of: PENDING, PROCESSED, FAILED")
	ErrWalletNotFound           = errors.New("wallet not found")
	ErrInsufficientFunds        = errors.New("insufficient funs")
	ErrIdempotencyConflict      = errors.New("idempotency conflict")
)

const (
	StatusPending   = "PENDING"
	StatusProcessed = "PROCESSED"
	StatusFailed    = "FAILED"
)

var validStatuses = map[string]struct{}{
	StatusPending:   {},
	StatusProcessed: {},
	StatusFailed:    {},
}

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
}

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWallet     string
	ToWallet       string
	Amount         int64
	Status         string
}

func MapToTransfer(t db.Transfer) Transfer {
	return Transfer{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWallet:     t.FromWalletID,
		ToWallet:       t.ToWalletID,
		Amount:         t.Amount,
		Status:         t.Status,
	}
}

func (tr *CreateTransferRequest) Validatation() error {
	if strings.TrimSpace(tr.IdempotencyKey) == "" {
		return ErrInvalidIdempotencyKey
	}
	if strings.TrimSpace(tr.FromWalletID) == "" {
		return ErrInvalidSourceWallet
	}
	if strings.TrimSpace(tr.ToWalletID) == "" {
		return ErrInvalidDestinationWallet
	}
	if strings.TrimSpace(tr.FromWalletID) == strings.TrimSpace(tr.ToWalletID) {
		return ErrSameWallet
	}
	if tr.Amount <= 0 {
		return ErrInvalidAmount
	}
	status := strings.TrimSpace(tr.Status)
	if status == "" {
		tr.Status = StatusPending
	} else {
		status = strings.ToUpper(status)
		if _, ok := validStatuses[status]; !ok {
			return ErrInvalidStatus
		}
		tr.Status = status
	}
	return nil
}
