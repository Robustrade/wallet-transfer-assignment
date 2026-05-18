package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-transfer-service/internal/domain"
	"wallet-transfer-service/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(service *service.TransferService) *TransferHandler {
	return &TransferHandler{
		service: service,
	}
}

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

func (h *TransferHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/transfers" {
		writeJSON(w, http.StatusNotFound, ErrorResponse{
			Error: "route not found",
		})
		return
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{
			Error: "method not allowed",
		})
		return
	}

	h.createTransfer(w, r)
}

func (h *TransferHandler) createTransfer(w http.ResponseWriter, r *http.Request) {
	var request CreateTransferRequest

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(&request); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{
			Error: "invalid JSON request body",
		})
		return
	}

	cmd := service.CreateTransferCommand{
		IdempotencyKey: request.IdempotencyKey,
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
	}

	result, err := h.service.CreateTransfer(r.Context(), cmd)
	if err != nil {
		statusCode, message := mapServiceError(err)

		writeJSON(w, statusCode, ErrorResponse{
			Error: message,
		})
		return
	}

	if result.Replayed {
		w.Header().Set("X-Idempotent-Replay", "true")
	} else {
		w.Header().Set("X-Idempotent-Replay", "false")
	}

	writeJSON(w, result.StatusCode, result.Response)
}

func mapServiceError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrMissingIdempotencyKey):
		return http.StatusBadRequest, "idempotencyKey is required"

	case errors.Is(err, domain.ErrInvalidWalletID):
		return http.StatusBadRequest, "wallet id is invalid"

	case errors.Is(err, domain.ErrSameWalletTransfer):
		return http.StatusBadRequest, "fromWalletId and toWalletId cannot be same"

	case errors.Is(err, domain.ErrInvalidAmount):
		return http.StatusBadRequest, "amount must be greater than zero"

	case errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict, "idempotency key was reused with a different request payload"

	case errors.Is(err, domain.ErrIdempotencyInProgress):
		return http.StatusConflict, "request with this idempotency key is still being processed"

	default:
		return http.StatusInternalServerError, "internal server error"
	}
}

func writeJSON(w http.ResponseWriter, statusCode int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	_ = json.NewEncoder(w).Encode(payload)
}
