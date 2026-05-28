package transfers

import (
	"context"
	"encoding/json"
	"errors"
)

type State string

const (
	StatePending   State = "PENDING"
	StateProcessed State = "PROCESSED"
	StateFailed    State = "FAILED"
)

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	State          State
	FailureReason  string
}

type CreateRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

type CreateResponse struct {
	TransferID string `json:"transferId"`
	State      State  `json:"state"`
}

var (
	ErrInvalidRequest      = errors.New("invalid request")
	ErrWalletNotFound      = errors.New("wallet not found")
	ErrInsufficientFunds   = errors.New("insufficient funds")
	ErrIdempotencyConflict = errors.New("idempotency key conflict")
)

type Service interface {
	Create(ctx context.Context, req CreateRequest) (status int, body json.RawMessage, err error)
}
