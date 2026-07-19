package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
)

type errorResponse struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, err error) {
	status, code := errorStatus(err)
	writeJSON(w, status, errorResponse{Error: code})
}

func errorStatus(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount):
		return http.StatusBadRequest, "INVALID_AMOUNT"
	case errors.Is(err, domain.ErrInvalidCurrency):
		return http.StatusBadRequest, "INVALID_CURRENCY"
	case errors.Is(err, domain.ErrInvalidIdempotency):
		return http.StatusBadRequest, "INVALID_IDEMPOTENCY_KEY"
	case errors.Is(err, domain.ErrInvalidWalletID):
		return http.StatusBadRequest, "INVALID_WALLET_ID"
	case errors.Is(err, domain.ErrSameWallet):
		return http.StatusBadRequest, "SAME_WALLET"
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound, "WALLET_NOT_FOUND"
	case errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound, "TRANSFER_NOT_FOUND"
	case errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict, "IDEMPOTENCY_KEY_REUSED"
	case errors.Is(err, domain.ErrRequestInProgress):
		return http.StatusConflict, "REQUEST_IN_PROGRESS"
	case errors.Is(err, domain.ErrWalletNotActive):
		return http.StatusUnprocessableEntity, "WALLET_NOT_ACTIVE"
	case errors.Is(err, domain.ErrCurrencyMismatch):
		return http.StatusUnprocessableEntity, "CURRENCY_MISMATCH"
	default:
		return http.StatusInternalServerError, "INTERNAL_ERROR"
	}
}
