package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	txService *service.TransferService
}

func NewTransferHandler(txService *service.TransferService) *TransferHandler {
	return &TransferHandler{txService: txService}
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	// Restrict payload size to 1 MB to prevent memory DOS
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req service.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "malformed or oversized json payload: "+err.Error())
		return
	}

	resp, statusCode, err := h.txService.ExecuteTransfer(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, model.ErrInvalidAmount),
			errors.Is(err, model.ErrSelfTransfer),
			errors.Is(err, model.ErrInvalidIdempotencyKey),
			errors.Is(err, model.ErrCurrencyMismatch):
			WriteError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
		case errors.Is(err, model.ErrWalletNotFound):
			WriteError(w, http.StatusNotFound, "NOT_FOUND", err.Error())
		case errors.Is(err, model.ErrInsufficientFunds):
			WriteError(w, http.StatusConflict, "INSUFFICIENT_FUNDS", err.Error())
		case errors.Is(err, model.ErrIdempotencyConflict):
			WriteError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT", err.Error())
		default:
			WriteError(w, statusCode, "TRANSFER_FAILED", err.Error())
		}
		return
	}

	WriteJSON(w, statusCode, resp)
}

// GetTransfer handles GET /transfers/{id}
func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[1] == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "transfer id is required")
		return
	}
	id := parts[1]

	resp, err := h.txService.GetTransfer(r.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrTransferNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "transfer not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, resp)
}
