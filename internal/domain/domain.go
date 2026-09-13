package domain

import (
	"strings"
	"time"
)

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

type TransferEventType string

const (
	DebitWalletEvent  TransferEventType = "DEBIT_WALLET"
	CreditWalletEvent TransferEventType = "CREDIT_WALLET"
)

type ValidationError struct {
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (r TransferRequest) Validate() error {
	if strings.TrimSpace(r.IdempotencyKey) == "" {
		return &ValidationError{Message: "idempotencyKey is required"}
	}

	if len(r.IdempotencyKey) > 255 {
		return &ValidationError{Message: "idempotencyKey must be at most 255 characters"}
	}

	if strings.TrimSpace(r.FromWalletID) == "" {
		return &ValidationError{Message: "fromWalletId is required"}
	}

	if strings.TrimSpace(r.ToWalletID) == "" {
		return &ValidationError{Message: "toWalletId is required"}
	}

	if len(r.FromWalletID) > 128 || len(r.ToWalletID) > 128 {
		return &ValidationError{Message: "wallet IDs must be at most 128 characters"}
	}

	if strings.TrimSpace(r.FromWalletID) != r.FromWalletID {
		return &ValidationError{Message: "fromWalletId must not have leading or trailing whitespace"}
	}

	if strings.TrimSpace(r.ToWalletID) != r.ToWalletID {
		return &ValidationError{Message: "toWalletId must not have leading or trailing whitespace"}
	}

	if r.FromWalletID == r.ToWalletID {
		return &ValidationError{Message: "fromWalletId and toWalletId must be different"}
	}

	if r.Amount <= 0 {
		return &ValidationError{Message: "amount must be greater than zero"}
	}

	return nil
}

type CreateWalletRequest struct {
	ID             string `json:"id"`
	Currency       string `json:"currency"`
	InitialBalance int64  `json:"initialBalance"`
}

func (r CreateWalletRequest) Validate() error {
	if strings.TrimSpace(r.ID) == "" {
		return &ValidationError{Message: "id is required"}
	}

	if len(r.ID) > 128 {
		return &ValidationError{Message: "id must be at most 128 characters"}
	}

	if strings.TrimSpace(r.ID) != r.ID {
		return &ValidationError{Message: "id must not have leading or trailing whitespace"}
	}

	if len(r.Currency) != 3 || r.Currency != strings.ToUpper(r.Currency) {
		return &ValidationError{Message: "currency must be a three-letter uppercase code"}
	}

	if r.InitialBalance < 0 {
		return &ValidationError{Message: "initialBalance cannot be negative"}
	}

	return nil
}

type Wallet struct {
	ID        string    `json:"id"`
	Currency  string    `json:"currency"`
	Balance   int64     `json:"balance"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type TransferResult struct {
	ID             string
	IdempotencyKey string
	RequestHash    string

	Status       TransferStatus
	FromWalletID string
	ToWalletID   string
	Amount       int64

	CreatedAt   time.Time
	ProcessedAt *time.Time

	FailureCode    string
	FailureMessage string

	ResponseCode int
	ResponseBody []byte
	Replayed     bool
}

type TransferEvent struct {
	ID             string
	TransferID     string
	IdempotencyKey string
	ShardID        int
	Type           TransferEventType
}

type TransferResponse struct {
	TransferID   string `json:"transferId"`
	Status       string `json:"status"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
}

type APIError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorResponse struct {
	Error APIError `json:"error"`
}
