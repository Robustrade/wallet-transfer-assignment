package domain

import (
	"strings"
	"time"
)

type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

const (
	FailureWalletNotActive   = "WALLET_NOT_ACTIVE"
	FailureInsufficientFunds = "INSUFFICIENT_FUNDS"
	FailureCurrencyMismatch  = "CURRENCY_MISMATCH"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Currency       string
	Status         TransferStatus
	FailureReason  *string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func NewTransfer(idempotencyKey, fromWalletID, toWalletID string, money Money) (Transfer, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return Transfer{}, ErrInvalidIdempotency
	}
	if strings.TrimSpace(fromWalletID) == "" || strings.TrimSpace(toWalletID) == "" {
		return Transfer{}, ErrInvalidWalletID
	}
	if fromWalletID == toWalletID {
		return Transfer{}, ErrSameWallet
	}
	return Transfer{
		IdempotencyKey: idempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         money.Amount,
		Currency:       money.Currency,
		Status:         TransferStatusPending,
	}, nil
}

func (transfer *Transfer) MarkProcessed() error {
	if transfer.Status != TransferStatusPending {
		return ErrInvalidState
	}
	transfer.Status = TransferStatusProcessed
	transfer.FailureReason = nil
	return nil
}

func (transfer *Transfer) MarkFailed(reason string) error {
	if transfer.Status != TransferStatusPending {
		return ErrInvalidState
	}
	transfer.Status = TransferStatusFailed
	transfer.FailureReason = &reason
	return nil
}

func (transfer Transfer) IsTerminal() bool {
	return transfer.Status == TransferStatusProcessed || transfer.Status == TransferStatusFailed
}
