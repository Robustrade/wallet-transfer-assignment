package http

import (
	"errors"
	"net/http"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// timeLayout is the timestamp format used in all API responses (RFC 3339).
const timeLayout = time.RFC3339Nano

// apiError captures the HTTP representation of a domain error.
type apiError struct {
	status  int
	code    string
	message string
	field   string
}

// mapError translates a domain or infrastructure error into an apiError,
// keeping all status-code policy in one place. Unknown errors degrade to a 500
// without leaking internal detail to the client.
func mapError(err error) apiError {
	var ve *domain.ValidationError
	if errors.As(err, &ve) {
		return apiError{status: http.StatusBadRequest, code: "VALIDATION_ERROR", message: ve.Message, field: ve.Field}
	}

	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		return apiError{status: http.StatusNotFound, code: "WALLET_NOT_FOUND", message: "wallet not found"}
	case errors.Is(err, domain.ErrTransferNotFound):
		return apiError{status: http.StatusNotFound, code: "TRANSFER_NOT_FOUND", message: "transfer not found"}
	case errors.Is(err, domain.ErrWalletAlreadyExists):
		return apiError{status: http.StatusConflict, code: "WALLET_ALREADY_EXISTS", message: "wallet already exists"}
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		return apiError{
			status:  http.StatusConflict,
			code:    "IDEMPOTENCY_KEY_CONFLICT",
			message: "idempotency key already used with different parameters",
		}
	default:
		return apiError{status: http.StatusInternalServerError, code: "INTERNAL_ERROR", message: "an unexpected error occurred"}
	}
}

// writeError maps err and writes the uniform error envelope.
func writeError(w http.ResponseWriter, err error) {
	ae := mapError(err)
	writeJSON(w, ae.status, errorResponse{Error: errorBody{
		Code:    ae.code,
		Message: ae.message,
		Field:   ae.field,
	}})
}
