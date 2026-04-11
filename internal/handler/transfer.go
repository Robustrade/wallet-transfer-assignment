package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	ID             string `json:"id"`
	IdempotencyKey string `json:"idempotencyKey,omitempty"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
	CreatedAt      string `json:"createdAt"`
	UpdatedAt      string `json:"updatedAt"`
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.FromWalletID == "" || req.ToWalletID == "" || req.Amount <= 0 {
		writeError(w, http.StatusBadRequest, "fromWalletId, toWalletId, and a positive amount are required")
		return
	}

	transfer, isNew, err := h.svc.CreateTransfer(r.Context(), service.CreateTransferRequest{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})

	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		if errors.Is(err, domain.ErrInsufficientFunds) {
			writeJSON(w, http.StatusUnprocessableEntity, toTransferResponse(transfer))
			return
		}
		if errors.Is(err, domain.ErrSameWallet) || errors.Is(err, domain.ErrInvalidAmount) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	status := http.StatusOK
	if isNew {
		status = http.StatusCreated
	}
	writeJSON(w, status, toTransferResponse(transfer))
}

func (h *TransferHandler) GetTransfer(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	transfer, err := h.svc.GetTransfer(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrTransferNotFound) {
			writeError(w, http.StatusNotFound, "transfer not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toTransferResponse(transfer))
}

func toTransferResponse(t *domain.Transfer) transferResponse {
	return transferResponse{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		CreatedAt:      t.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      t.UpdatedAt.Format(time.RFC3339),
	}
}
