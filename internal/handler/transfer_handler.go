package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/domain"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/repository"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/service"
)

// transferCreator is the service behavior the handler depends on.
type transferCreator interface {
	CreateTransfer(ctx context.Context, req service.TransferRequest) (*domain.Transfer, bool, error)
}

// TransferHandler handles transfer HTTP requests.
type TransferHandler struct {
	svc transferCreator
}

// NewTransferHandler builds a handler.
func NewTransferHandler(svc transferCreator) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// createTransferRequest is the wire format for POST /transfers.
type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// transferResponse is the wire format returned to clients.
type transferResponse struct {
	ID           string `json:"id"`
	FromWalletID string `json:"fromWalletId"`
	ToWalletID   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
	Status       string `json:"status"`
}

// errorResponse is a uniform error body.
type errorResponse struct {
	Error string `json:"error"`
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	if body.IdempotencyKey == "" {
		writeError(w, http.StatusBadRequest, "idempotencyKey is required")
		return
	}

	tr, created, err := h.svc.CreateTransfer(r.Context(), service.TransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, transferResponse{
		ID:           tr.ID,
		FromWalletID: tr.FromWalletID,
		ToWalletID:   tr.ToWalletID,
		Amount:       tr.Amount,
		Status:       string(tr.Status),
	})
}

// writeServiceError maps domain/service/repo errors to HTTP status codes.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrMissingWallet):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, repository.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, service.ErrInsufficientBalance):
		writeError(w, http.StatusUnprocessableEntity, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal error")
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorResponse{Error: msg})
}