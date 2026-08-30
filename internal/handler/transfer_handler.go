package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(
	service *service.TransferService,
) *TransferHandler {
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
	if r.Method != http.MethodPost {
		http.Error(
			w,
			"method not allowed",
			http.StatusMethodNotAllowed,
		)
		return
	}

	var req createTransferRequest

	decoder := json.NewDecoder(r.Body)

	if err := decoder.Decode(&req); err != nil {
		http.Error(
			w,
			"invalid request body",
			http.StatusBadRequest,
		)
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
		if result != nil && errors.Is(err, service.ErrInsufficientBalance) {
			writeJSON(w, http.StatusUnprocessableEntity, result)
			return
		}

		handleTransferError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, result)

}

func handleTransferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrInvalidRequest):
		http.Error(
			w,
			err.Error(),
			http.StatusBadRequest,
		)

	case errors.Is(err, service.ErrWalletNotFound):
		http.Error(
			w,
			err.Error(),
			http.StatusNotFound,
		)

	case errors.Is(err, service.ErrInsufficientBalance):
		http.Error(
			w,
			err.Error(),
			http.StatusUnprocessableEntity,
		)

	case errors.Is(err, service.ErrIdempotencyConflict):
		http.Error(
			w,
			err.Error(),
			http.StatusConflict,
		)

	default:
		http.Error(
			w,
			"internal server error",
			http.StatusInternalServerError,
		)
	}
}

func writeJSON(
	w http.ResponseWriter,
	status int,
	data any,
) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	_ = json.NewEncoder(w).Encode(data)
}
