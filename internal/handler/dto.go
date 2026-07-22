package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/shopspring/decimal"
	"wallet-transfer/internal/domain"
)

// createTransferRequest is the POST /transfers body. amount accepts a JSON
// number or string; decimal keeps it exact.
type createTransferRequest struct {
	IdempotencyKey string          `json:"idempotencyKey"`
	FromWalletID   string          `json:"fromWalletId"`
	ToWalletID     string          `json:"toWalletId"`
	Amount         decimal.Decimal `json:"amount"`
}

// transferResponse is the transfer resource returned to clients.
type transferResponse struct {
	ID             string          `json:"id"`
	IdempotencyKey string          `json:"idempotencyKey"`
	FromWalletID   string          `json:"fromWalletId"`
	ToWalletID     string          `json:"toWalletId"`
	Amount         decimal.Decimal `json:"amount"`
	State          string          `json:"state"`
	FailureReason  *string         `json:"failureReason,omitempty"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
}

func toTransferResponse(t domain.Transfer) transferResponse {
	// PROCESSING is an internal claim state. The assignment's documented states are
	// PENDING, PROCESSED, and FAILED, so a claimed transfer is shown as PENDING
	// (not yet finished) to callers.
	state := t.State
	if state == domain.StateProcessing {
		state = domain.StatePending
	}
	return transferResponse{
		ID:             t.ID.String(),
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID.String(),
		ToWalletID:     t.ToWalletID.String(),
		Amount:         t.Amount,
		State:          string(state),
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

type errorResponse struct {
	Error string `json:"error"`
}

func respondJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, errorResponse{Error: msg})
}
