package controller

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
)

// writeDomainError maps a domain/service error to the appropriate HTTP
// status code and a machine-readable error code. Unrecognized errors are
// logged with detail and returned to the client as an opaque 500.
func writeDomainError(w http.ResponseWriter, logger *slog.Logger, err error) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		WriteError(w, logger, http.StatusNotFound, "wallet_not_found", err.Error())
	case errors.Is(err, domain.ErrTransferNotFound):
		WriteError(w, logger, http.StatusNotFound, "transfer_not_found", err.Error())
	case errors.Is(err, domain.ErrWalletAlreadyExists):
		WriteError(w, logger, http.StatusConflict, "wallet_already_exists", err.Error())
	case errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrIdempotencyKeyRequired),
		errors.Is(err, domain.ErrInvalidWalletID):
		WriteError(w, logger, http.StatusBadRequest, "invalid_request", err.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		WriteError(w, logger, http.StatusConflict, "idempotency_key_conflict", err.Error())
	case errors.Is(err, domain.ErrIdempotencyInProgress):
		w.Header().Set("Retry-After", "1")
		WriteError(w, logger, http.StatusConflict, "idempotency_in_progress", err.Error())
	default:
		logger.Error("unhandled request error", "error", err)
		WriteError(w, logger, http.StatusInternalServerError, "internal_error", "an unexpected error occurred")
	}
}
