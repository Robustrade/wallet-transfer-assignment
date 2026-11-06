package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// apiError is an error that already knows its HTTP representation.
type apiError struct {
	status  int
	code    string
	message string
}

func (e *apiError) Error() string { return e.code + ": " + e.message }

func badRequest(message string) *apiError {
	return &apiError{status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: message}
}

// toAPIError maps domain and infrastructure errors to their HTTP representation.
// Unknown errors become a generic 500 so internal details never reach clients.
func toAPIError(err error) *apiError {
	var (
		apiErr *apiError
		verr   *domain.ValidationError
	)
	switch {
	case errors.As(err, &apiErr):
		return apiErr
	case errors.As(err, &verr):
		return badRequest(verr.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		return &apiError{http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found"}
	case errors.Is(err, domain.ErrTransferNotFound):
		return &apiError{http.StatusNotFound, "TRANSFER_NOT_FOUND", "transfer not found"}
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		return &apiError{http.StatusUnprocessableEntity, "IDEMPOTENCY_KEY_REUSED",
			"idempotencyKey was already used with a different request"}
	case errors.Is(err, domain.ErrBusy), errors.Is(err, context.DeadlineExceeded):
		return &apiError{http.StatusServiceUnavailable, "SERVICE_BUSY", "the service is busy; retry later"}
	default:
		return &apiError{http.StatusInternalServerError, "INTERNAL_ERROR", "internal error"}
	}
}

func writeError(w http.ResponseWriter, r *http.Request, logger *slog.Logger, err error) {
	apiErr := toAPIError(err)
	if apiErr.status >= http.StatusInternalServerError {
		logger.ErrorContext(r.Context(), "request failed",
			slog.String("code", apiErr.code), slog.String("error", err.Error()))
	}
	if apiErr.status == http.StatusServiceUnavailable {
		w.Header().Set("Retry-After", "1")
	}
	writeJSON(w, apiErr.status, errorResponse{Error: errorBody{Code: apiErr.code, Message: apiErr.message}})
}
