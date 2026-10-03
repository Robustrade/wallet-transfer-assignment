package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/malem-mayeng/wallet-transfer-assignment/internal/domain"
	"github.com/malem-mayeng/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(transferService *service.TransferService) *TransferHandler {
	return &TransferHandler{
		service: transferService,
	}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	TransferID     int64  `json:"transferId"`
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Status         string `json:"status"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req createTransferRequest

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid request body",
		})
		return
	}

	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid request body",
		})
		return
	}

	if decoder.More() {
		writeJSON(w, http.StatusBadRequest, errorResponse{
			Error: "invalid request body",
		})
		return
	}

	result, err := h.service.CreateTransfer(
		r.Context(),
		service.CreateTransferRequest{
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
		},
	)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidAmount),
			errors.Is(err, domain.ErrSameWallet),
			errors.Is(err, domain.ErrInvalidIdempotencyKey),
			errors.Is(err, domain.ErrInvalidWalletID):
			writeJSON(w, http.StatusBadRequest, errorResponse{
				Error: err.Error(),
			})

		case errors.Is(err, domain.ErrIdempotencyConflict):
			writeJSON(w, http.StatusConflict, errorResponse{
				Error: err.Error(),
			})

		case errors.Is(err, domain.ErrWalletNotFound):
			writeJSON(w, http.StatusNotFound, errorResponse{
				Error: err.Error(),
			})

		default:
			writeJSON(w, http.StatusInternalServerError, errorResponse{
				Error: "internal server error",
			})
		}

		return
	}

	writeJSON(w, http.StatusOK, transferResponse{
		TransferID:     result.ID,
		IdempotencyKey: result.IdempotencyKey,
		FromWalletID:   result.FromWalletID,
		ToWalletID:     result.ToWalletID,
		Amount:         result.Amount,
		Status:         string(result.Status),
	})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(value)
}
