// Package handler contains thin HTTP adapters. No business logic or SQL
// lives here — only request decoding, response encoding, and status-code
// mapping from domain errors.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/service"
)

// transferCreator is the narrow slice of *service.TransferService this
// handler needs. Depending on an interface defined at the consumer (rather
// than importing the concrete service type directly) keeps the handler
// layer unit-testable with a fake, with zero database involved.
type transferCreator interface {
	CreateTransfer(ctx context.Context, req domain.NewTransferRequest) (service.TransferResult, error)
}

type TransferHandler struct {
	svc transferCreator
}

func NewTransferHandler(svc transferCreator) *TransferHandler {
	return &TransferHandler{svc: svc}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	TransferID    string `json:"transferId"`
	Status        string `json:"status"`
	FromWalletID  string `json:"fromWalletId"`
	ToWalletID    string `json:"toWalletId"`
	Amount        int64  `json:"amount"`
	FailureReason string `json:"failureReason,omitempty"`
	Replayed      bool   `json:"replayed"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "invalid JSON body"})
		return
	}

	result, err := h.svc.CreateTransfer(r.Context(), domain.NewTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		writeJSON(w, statusForError(err), errorResponse{
			Error: publicError(err),
		})
		return
	}

	t := result.Transfer
	status := http.StatusOK
	if !result.Replayed {
		status = http.StatusCreated
	}
	writeJSON(w, status, transferResponse{
		TransferID:   t.ID,
		Status:       string(t.Status),
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       t.Amount,
		Replayed:     result.Replayed,
	})
}

// statusForError maps domain-level sentinel errors to HTTP status codes.
// Keeping this mapping in the handler (not the service) is what lets the
// service package stay transport-agnostic.
func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrIdempotencyKeyEmpty),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrWalletIdEmpty),
		errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict
	case errors.Is(err, domain.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func publicError(err error) string {
	switch {
	case errors.Is(err, domain.ErrIdempotencyKeyEmpty):
		return "idempotency key is required"
	case errors.Is(err, domain.ErrSameWallet):
		return "source and destination wallets must be different"
	case errors.Is(err, domain.ErrInvalidAmount):
		return "amount must be greater than zero"
	case errors.Is(err, domain.ErrWalletNotFound):
		return "wallet not found"
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return "idempotency key already used with a different request"
	case errors.Is(err, domain.ErrInsufficientFunds):
		return "insufficient funds"
	case errors.Is(err, domain.ErrWalletIdEmpty):
		return "wallet id is empty"
	default:
		return "internal server error"
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
