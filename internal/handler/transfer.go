package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/example/wallet-transfer-assignment/internal/domain"
	"github.com/example/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{service: svc}
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.service.CreateTransfer(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidAmount), errors.Is(err, domain.ErrSameWallet):
			respondError(w, http.StatusBadRequest, err.Error())
		case errors.Is(err, domain.ErrInsufficientFunds):
			respondError(w, http.StatusUnprocessableEntity, err.Error())
		default:
			respondError(w, http.StatusInternalServerError, "internal error")
		}
		return
	}

	status := http.StatusCreated
	if resp.Message == "duplicate request" {
		status = http.StatusOK
	}
	respondJSON(w, status, resp)
}
