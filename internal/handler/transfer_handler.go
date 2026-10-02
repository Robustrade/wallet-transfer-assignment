package handler

import (
	"context"
	"encoding/json"
	"net/http"

	"wallet-service/internal/domain"
	"wallet-service/internal/repository"
	"wallet-service/internal/service"
)

type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type TransferResponse struct {
	TransferID string `json:"transferId"`
	State      string `json:"state"`
	Error      string `json:"error,omitempty"`
}

type TransferHandler struct {
	service service.TransferService
	repo    repository.Repository
}

func NewTransferHandler(service service.TransferService, repo repository.Repository) *TransferHandler {
	return &TransferHandler{service: service, repo: repo}
}

func (h *TransferHandler) HandleTransfer(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	var req TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeError(ctx, w, http.StatusBadRequest, "invalid request body", "")
		return
	}

	if req.IdempotencyKey == "" {
		h.writeError(ctx, w, http.StatusBadRequest, "idempotencyKey is required", "")
		return
	}

	transfer, err := h.service.Transfer(ctx, req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount)

	if err != nil {
		statusCode := http.StatusInternalServerError
		if err == domain.ErrWalletNotFound || err == domain.ErrInsufficientFunds || err == domain.ErrInvalidAmount || err == domain.ErrSameWallet {
			statusCode = http.StatusBadRequest
		}
		h.writeError(ctx, w, statusCode, err.Error(), req.IdempotencyKey)
		return
	}

	resp := TransferResponse{
		TransferID: transfer.ID,
		State:      string(transfer.State),
	}

	h.writeResponse(ctx, w, http.StatusOK, resp, req.IdempotencyKey)
}

func (h *TransferHandler) writeError(ctx context.Context, w http.ResponseWriter, statusCode int, message string, idempotencyKey string) {
	resp := TransferResponse{Error: message}
	h.writeResponse(ctx, w, statusCode, resp, idempotencyKey)
}

func (h *TransferHandler) writeResponse(ctx context.Context, w http.ResponseWriter, statusCode int, resp TransferResponse, idempotencyKey string) {
	respBytes, _ := json.Marshal(resp)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	w.Write(respBytes)
}
