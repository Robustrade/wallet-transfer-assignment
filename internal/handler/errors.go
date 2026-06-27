package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
)

// errorBody is the JSON error envelope returned to clients:
//
//	{"error":{"code":"...","message":"..."}}
type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// writeJSON encodes v as JSON with the given status code. Encoding failures are
// logged server-side; the status line has already been written by that point.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("handler: failed to encode response: %v", err)
	}
}

// statusForError maps a domain error onto an HTTP status and a stable,
// machine-readable error code. The returned message is safe to expose; for
// unmapped errors the caller substitutes a generic message.
func statusForError(err error) (status int, code, message string) {
	switch {
	case errors.Is(err, domain.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity, "insufficient_funds", err.Error()
	case errors.Is(err, domain.ErrIdempotencyKeyReuse):
		return http.StatusConflict, "idempotency_key_reuse", err.Error()
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrMissingIdempotencyKey),
		errors.Is(err, domain.ErrMissingWalletID):
		return http.StatusBadRequest, "invalid_request", err.Error()
	case errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound, "not_found", err.Error()
	default:
		return http.StatusInternalServerError, "internal_error", "an internal error occurred"
	}
}

// writeError maps a domain error onto an HTTP status code and JSON error
// envelope. Unmapped errors are surfaced as a generic 500 and the underlying
// error is logged server-side so internal details never leak to clients.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, code, message := statusForError(err)
	if status == http.StatusInternalServerError {
		log.Printf("handler: internal error on %s %s: %v", r.Method, r.URL.Path, err)
	}
	writeJSON(w, status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

// writeBadRequest emits a 400 invalid_request envelope with an explicit message,
// used for transport-level decoding failures that have no domain error.
func writeBadRequest(w http.ResponseWriter, message string) {
	writeJSON(w, http.StatusBadRequest, errorBody{Error: errorDetail{Code: "invalid_request", Message: message}})
}
