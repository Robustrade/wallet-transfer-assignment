package handler

import (
	"errors"
	"net/http"

	"wallet-transfer/internal/domain"
)

// statusForError maps a domain/service error to an HTTP status. Unknown errors
// are treated as 500 (and logged by the caller).
func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrEmptyIdempotencyKey),
		errors.Is(err, domain.ErrIdempotencyKeyTooLong),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, domain.ErrWalletInactive),
		errors.Is(err, domain.ErrInsufficientFunds):
		// Well-formed request that cannot be enqueued (e.g. unknown wallet).
		return http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound
	default:
		return http.StatusInternalServerError
	}
}
