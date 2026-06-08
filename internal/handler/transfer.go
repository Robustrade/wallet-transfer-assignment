package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/aks-95/wallet-transfer-assignment/internal/domain"
	"github.com/aks-95/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(service *service.TransferService) *TransferHandler {
	return &TransferHandler{service: service}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	ID             string  `json:"id"`
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Status         string  `json:"status"`
	FailureReason  *string `json:"failureReason"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var body createTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	result, err := h.service.CreateTransfer(r.Context(), domain.CreateTransferRequest{
		IdempotencyKey: body.IdempotencyKey,
		FromWalletID:   body.FromWalletID,
		ToWalletID:     body.ToWalletID,
		Amount:         body.Amount,
	})
	if err != nil {
		status, message := mapTransferError(err)
		if result != nil && result.Transfer != nil {
			writeJSON(w, status, toTransferResponse(result.Transfer))
			return
		}
		writeError(w, status, message)
		return
	}

	status := http.StatusCreated
	if result.Replayed {
		status = http.StatusOK
	}
	writeJSON(w, status, toTransferResponse(result.Transfer))
}

func mapTransferError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrInvalidIdempotencyKey),
		errors.Is(err, domain.ErrInvalidWalletID),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, service.ErrWalletNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, service.ErrIdempotencyConflict):
		return http.StatusConflict, err.Error()
	case errors.Is(err, service.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, err.Error()
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}

func toTransferResponse(transfer *domain.Transfer) transferResponse {
	return transferResponse{
		ID:             transfer.ID.String(),
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletID:   transfer.FromWalletID.String(),
		ToWalletID:     transfer.ToWalletID.String(),
		Amount:         transfer.Amount,
		Status:         string(transfer.Status),
		FailureReason:  transfer.FailureReason,
		CreatedAt:      transfer.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      transfer.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
