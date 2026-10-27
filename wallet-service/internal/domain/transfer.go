package domain

import (
	"strings"
	"unicode/utf8"
)

type Status string

const (
	Pending   Status = "PENDING"
	Processed Status = "PROCESSED"
	Failed    Status = "FAILED"
)

// Payload is the normalized identity of a transfer, excluding its key.
type Payload struct {
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
}

type Request struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type Transfer struct {
	ID string
	Payload
	Currency string
	Status   Status
}

func ValidateRequest(r Request) error {
	for _, value := range []string{r.IdempotencyKey, r.FromWalletID, r.ToWalletID} {
		if value == "" || !utf8.ValidString(value) || strings.ContainsRune(value, '\x00') {
			return ErrInvalidRequest
		}
	}
	if utf8.RuneCountInString(r.IdempotencyKey) > 128 || r.Amount <= 0 {
		return ErrInvalidRequest
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSelfTransfer
	}
	return nil
}

func ValidateTransition(current, next Status, failureCode string) error {
	if current == Pending {
		if next == Processed && failureCode == "" {
			return nil
		}
		if next == Failed && (failureCode == string(ErrInsufficientFunds) || failureCode == string(ErrBalanceOverflow)) {
			return nil
		}
	}
	return ErrInvalidTransition
}
