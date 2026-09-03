package service

import (
	"encoding/json"
	"time"

	"github.com/rohitkumar27/wallet-transfer-assignment/internal/domain"
)

// TransferResponse is the JSON shape returned for a created or replayed
// transfer. It is also what gets persisted verbatim into
// idempotency_records.response_body so a replay returns byte-identical
// output to the original.
type TransferResponse struct {
	ID            string  `json:"id"`
	Status        string  `json:"status"`
	FromWalletID  string  `json:"fromWalletId"`
	ToWalletID    string  `json:"toWalletId"`
	Amount        int64   `json:"amount"`
	FailureReason *string `json:"failureReason,omitempty"`
	CreatedAt     string  `json:"createdAt"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// Result is what the service returns to the handler: an HTTP status and a
// pre-serialized body, so the same code path renders both a fresh result
// and an idempotent replay.
type Result struct {
	StatusCode int
	Body       json.RawMessage
}

func transferResult(statusCode int, t *domain.Transfer) (*Result, error) {
	body, err := json.Marshal(TransferResponse{
		ID:            t.ID.String(),
		Status:        string(t.Status),
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		FailureReason: t.FailureReason,
		CreatedAt:     t.CreatedAt.UTC().Format(time.RFC3339),
	})
	if err != nil {
		return nil, err
	}
	return &Result{StatusCode: statusCode, Body: body}, nil
}

func errorResult(statusCode int, message string) (*Result, error) {
	body, err := json.Marshal(ErrorResponse{Error: message})
	if err != nil {
		return nil, err
	}
	return &Result{StatusCode: statusCode, Body: body}, nil
}
