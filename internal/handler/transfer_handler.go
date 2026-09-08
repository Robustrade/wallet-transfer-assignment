package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{service: svc}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid json body")
		return
	}

	transfer, err := h.service.ExecuteTransfer(r.Context(), req)
	if err != nil {
		if errors.Is(err, domain.ErrEmptyIdempotencyKey) ||
			errors.Is(err, domain.ErrEmptyWalletID) ||
			errors.Is(err, domain.ErrSameWalletTransfer) ||
			errors.Is(err, domain.ErrInvalidAmount) {
			writeJSONError(w, http.StatusBadRequest, "VALIDATION_ERROR", err.Error())
			return
		}
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSONError(w, http.StatusUnprocessableEntity, "WALLET_NOT_FOUND", err.Error())
			return
		}
		if errors.Is(err, domain.ErrInsufficientFunds) {
			writeJSONError(w, http.StatusUnprocessableEntity, "INSUFFICIENT_FUNDS", err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, transfer)
}
