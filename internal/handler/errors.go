package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
)

// mapDomainErrorToHTTPStatus maps domain error codes to HTTP status codes.
func mapDomainErrorToHTTPStatus(err *domain.DomainError) int {
	switch err.Code {
	case "INVALID_INPUT":
		return http.StatusBadRequest
	case "WALLET_NOT_FOUND", "TRANSFER_NOT_FOUND":
		return http.StatusNotFound
	case "INSUFFICIENT_FUNDS", "IDEMPOTENCY_KEY_MISMATCH":
		return http.StatusUnprocessableEntity
	case "IDEMPOTENCY_KEY_IN_PROGRESS", "DUPLICATE_WALLET":
		return http.StatusConflict
	case "RETRYABLE":
		return http.StatusServiceUnavailable
	case "PREVIOUS_REQUEST_FAILED":
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

// handleServiceError maps service errors to appropriate HTTP error responses.
// This is the single shared implementation used by all handlers.
func handleServiceError(w http.ResponseWriter, logger *slog.Logger, err error) {
	var domainErr *domain.DomainError
	if errors.As(err, &domainErr) {
		status := mapDomainErrorToHTTPStatus(domainErr)
		writeError(w, status, domainErr.Code, domainErr.Message)
		return
	}

	// Unexpected internal error — log details but return generic message to client
	logger.Error("internal server error", "error", err)
	writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an internal error occurred")
}
