package domain

import (
	"crypto/rand"
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

const FailureInsufficientFunds = "INSUFFICIENT_FUNDS"

func (s TransferStatus) IsTerminal() bool {
	return s == StatusProcessed || s == StatusFailed
}

type Transfer struct {
	ID            string
	FromWalletID  string
	ToWalletID    string
	Amount        int64
	Status        TransferStatus
	FailureReason string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

func (r TransferRequest) Validate() error {
	switch {
	case r.IdempotencyKey == "":
		return fmt.Errorf("%w: idempotencyKey", ErrMissingField)
	case r.FromWalletID == "":
		return fmt.Errorf("%w: fromWalletId", ErrMissingField)
	case r.ToWalletID == "":
		return fmt.Errorf("%w: toWalletId", ErrMissingField)
	case r.Amount <= 0:
		return ErrInvalidAmount
	case r.FromWalletID == r.ToWalletID:
		return ErrSameWallet
	}
	return nil
}

func (r TransferRequest) Hash() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", r.FromWalletID, r.ToWalletID, r.Amount)))
	return hex.EncodeToString(sum[:])
}

func NewTransfer(req TransferRequest, now time.Time) *Transfer {
	return &Transfer{
		ID:           NewID("tr"),
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
		Status:       StatusPending,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
}

func NewID(prefix string) string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("wallet: cannot read random bytes: %v", err))
	}
	return prefix + "_" + hex.EncodeToString(b[:])
}
