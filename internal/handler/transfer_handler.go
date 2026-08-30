package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"wallet-transfer/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(service *service.TransferService) *TransferHandler {
	return &TransferHandler{
		service: service,
	}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

func (h *TransferHandler) CreateTransfer(
	w http.ResponseWriter,
	r *http.Request,
) {

	var req createTransferRequest

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	fromWalletID, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		http.Error(w, "invalid fromWalletId", http.StatusBadRequest)
		return
	}

	toWalletID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		http.Error(w, "invalid toWalletId", http.StatusBadRequest)
		return
	}

	transfer, err := h.service.CreateTransfer(
		r.Context(),
		service.TransferRequest{
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   fromWalletID,
			ToWalletID:     toWalletID,
			Amount:         req.Amount,
		},
	)

	if err != nil {
		if errors.Is(err, service.ErrInsufficientFunds) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(transfer)
			return
		}

		http.Error(
			w,
			err.Error(),
			http.StatusInternalServerError,
		)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	_ = json.NewEncoder(w).Encode(transfer)
}
