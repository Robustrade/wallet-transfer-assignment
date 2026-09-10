package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

// Transfer is the durable record of a wallet-to-wallet movement.
// Status starts as PENDING in memory and is written only after it
// becomes PROCESSED or FAILED.
type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type CreateTransferInput struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Now            time.Time
}

func NewTransfer(in CreateTransferInput) (Transfer, error) {
	if in.IdempotencyKey == "" {
		return Transfer{}, ErrMissingIdempotencyKey
	}
	if in.FromWalletID == "" || in.ToWalletID == "" {
		return Transfer{}, ErrMissingWalletID
	}
	if in.FromWalletID == in.ToWalletID {
		return Transfer{}, ErrSameWallet
	}
	if in.Amount <= 0 {
		return Transfer{}, ErrInvalidAmount
	}
	now := in.Now.UTC()
	return Transfer{
		ID:             in.ID,
		IdempotencyKey: in.IdempotencyKey,
		FromWalletID:   in.FromWalletID,
		ToWalletID:     in.ToWalletID,
		Amount:         in.Amount,
		Status:         StatusPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}, nil
}

// RequestHash fingerprints the business payload so a reused key with a
// different transfer is rejected instead of replayed.
func RequestHash(fromWalletID, toWalletID string, amount int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", fromWalletID, toWalletID, amount)))
	return hex.EncodeToString(sum[:])
}

func (t Transfer) CanTransitionTo(next TransferStatus) bool {
	return t.Status == StatusPending && (next == StatusProcessed || next == StatusFailed)
}

func (t Transfer) MarkProcessed(now time.Time) (Transfer, error) {
	if !t.CanTransitionTo(StatusProcessed) {
		return Transfer{}, ErrIllegalTransition
	}
	t.Status = StatusProcessed
	t.FailureReason = ""
	t.UpdatedAt = now.UTC()
	return t, nil
}

func (t Transfer) MarkFailed(reason string, now time.Time) (Transfer, error) {
	if !t.CanTransitionTo(StatusFailed) {
		return Transfer{}, ErrIllegalTransition
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	t.UpdatedAt = now.UTC()
	return t, nil
}

type IdempotencyRecord struct {
	IdempotencyKey string
	TransferID     string
	RequestHash    string
	CreatedAt      time.Time
}
