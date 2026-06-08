package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/benjamin-benny/wallet-transfer/internal/domain"
)

// httpStatusForError maps domain and validation errors to HTTP status codes.
// Unexpected errors are logged and mapped to 500.
func httpStatusForError(err error) (status int, msg string) {
	switch {
	case errors.Is(err, domain.ErrMissingIdempotencyKey),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount):
		return http.StatusBadRequest, err.Error()

	case errors.Is(err, domain.ErrIdempotencyConflict):
		return http.StatusConflict, "idempotency_key_reuse_with_different_payload"

	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusUnprocessableEntity, err.Error()

	default:
		slog.Error("unexpected service error", "err", err)
		return http.StatusInternalServerError, "internal server error"
	}
}
