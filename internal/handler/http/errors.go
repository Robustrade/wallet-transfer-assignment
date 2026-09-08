package http

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// statusForError maps a domain error to an HTTP status and a stable machine code.
func statusForError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, "validation_error"
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound, "wallet_not_found"
	case errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound, "transfer_not_found"
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		return http.StatusConflict, "idempotency_key_conflict"
	case errors.Is(err, domain.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, "insufficient_funds"
	case errors.Is(err, domain.ErrCurrencyMismatch):
		return http.StatusUnprocessableEntity, "currency_mismatch"
	default:
		return http.StatusInternalServerError, "internal_error"
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body != nil {
		_ = json.NewEncoder(w).Encode(body)
	}
}

func writeError(w http.ResponseWriter, log *slog.Logger, err error) {
	status, code := statusForError(err)
	if status >= http.StatusInternalServerError {
		log.Error("request failed", "err", err.Error(), "code", code)
		writeJSON(w, status, errorResponse{Error: "internal error", Code: code})
		return
	}
	writeJSON(w, status, errorResponse{Error: err.Error(), Code: code})
}
