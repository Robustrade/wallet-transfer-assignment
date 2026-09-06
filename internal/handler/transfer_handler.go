package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	transferService service.TransferService
}

func NewTransferHandler(transferService service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r http.Request) {
	var req domain.CreateTransferRequest

	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	response, err := h.transferService.CreateTransfer(r.Context(), req)
	if err != nil {
		h.handleError(w, err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	err = json.NewEncoder(w).Encode(response)
	if err != nil {
		return
	}
}

func (h *TransferHandler) handleError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInsufficientFunds),
		errors.Is(err, service.ErrInvalidRequest):
		http.Error(w, err.Error(), http.StatusBadRequest)

	case errors.Is(err, service.ErrWalletNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)

	default:
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}
}
