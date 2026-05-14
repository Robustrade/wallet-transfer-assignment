package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// POST /transfers
func (h *Handler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req createTransferRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}

	if req.IdempotencyKey == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"idempotencyKey is required"})
		return
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{"fromWalletId and toWalletId are required"})
		return
	}
	if req.Amount <= 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{"amount must be greater than zero"})
		return
	}

	transfer, err := h.svc.CreateTransfer(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInsufficientFunds):
			// Return the FAILED transfer so the client can inspect the record.
			writeJSON(w, http.StatusUnprocessableEntity, transfer)
		case errors.Is(err, domain.ErrWalletNotFound):
			writeJSON(w, http.StatusNotFound, errorResponse{err.Error()})
		case errors.Is(err, domain.ErrMissingIdempotencyKey),
			errors.Is(err, domain.ErrSameWallet),
			errors.Is(err, domain.ErrInvalidAmount):
			writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
		default:
			slog.Error("create transfer failed", "error", err)
			writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error"})
		}
		return
	}

	writeJSON(w, http.StatusCreated, transfer)
}
