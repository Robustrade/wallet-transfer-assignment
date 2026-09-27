package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/model"
	"wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(service *service.TransferService) *TransferHandler {
	return &TransferHandler{service: service}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req model.TransferRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}

	if req.IdempotencyKey == "" ||
		req.FromWalletID == "" ||
		req.ToWalletID == "" {
		http.Error(w, "missing required fields", http.StatusBadRequest)
		return
	}

	transfer, err := h.service.Transfer(req)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrInvalidAmount),
			errors.Is(err, service.ErrSameWallet):
			http.Error(w, err.Error(), http.StatusBadRequest)

		case errors.Is(err, service.ErrWalletNotFound):
			http.Error(w, err.Error(), http.StatusNotFound)

		case errors.Is(err, service.ErrInsufficientFunds):
			http.Error(w, err.Error(), http.StatusBadRequest)

		default:
			http.Error(w, "transfer failed", http.StatusInternalServerError)
		}

		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(transfer)
}
