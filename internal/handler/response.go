package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	appErrors "wallet-transfer-assignment/internal/appErrors"
)

type ErrorResponse struct {
	Error APIError `json:"error"`
}

type APIError struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId,omitempty"`
}

func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, body := mapError(err)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if writeErr := json.NewEncoder(w).Encode(ErrorResponse{Error: body}); writeErr != nil {
		slog.ErrorContext(r.Context(), "write error response failed", "error", writeErr)
	}
}

func mapError(err error) (int, APIError) {
	switch {
	case errors.Is(err, appErrors.ErrInvalidTransfer):
		return http.StatusBadRequest, APIError{Code: "VALIDATION_ERROR", Message: "Invalid transfer request."}
	case errors.Is(err, appErrors.ErrWalletNotFound):
		return http.StatusNotFound, APIError{Code: "WALLET_NOT_FOUND", Message: "Wallet not found."}
	case errors.Is(err, appErrors.ErrIdempotencyConflict):
		return http.StatusConflict, APIError{Code: "IDEMPOTENCY_KEY_REUSED", Message: "Idempotency key belongs to a different transfer request."}
	case errors.Is(err, appErrors.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, APIError{Code: "INSUFFICIENT_FUNDS", Message: "Source wallet has insufficient funds."}
	case errors.Is(err, appErrors.ErrTransferTemporarilyUnavailable), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, APIError{Code: "TRANSFER_TEMPORARILY_UNAVAILABLE", Message: "Retry using the same idempotency key and payload."}
	case errors.Is(err, appErrors.ErrServiceUnavailable), errors.Is(err, context.Canceled):
		return http.StatusServiceUnavailable, APIError{Code: "SERVICE_UNAVAILABLE", Message: "Transfer outcome could not be confirmed. Retry using the same idempotency key and payload."}
	default:
		return http.StatusInternalServerError, APIError{Code: "INTERNAL_ERROR", Message: "Transfer outcome could not be confirmed. Retry using the same idempotency key and payload."}
	}
}
