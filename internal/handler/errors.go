package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
)

// errorBody is the wire shape of every error response. Keeping it
// boring and consistent makes it easy for clients to handle errors
// uniformly: always a JSON object with a single "error" string field.
type errorBody struct {
	Error string `json:"error"`
}

// writeError maps domain/repository errors onto HTTP status codes and
// writes a JSON body. Handlers stay thin by funnelling every error
// response through this function — there is no other place in the
// codebase that decides what status code a domain error becomes.
//
// 5xx responses additionally emit a structured log line with the full
// error so operators can debug, while the client only sees the
// generic "internal error" message. This is the only place where
// server-side error detail (which can include schema names, DSN
// fragments, etc.) is allowed to surface in logs — see AGENTS.md §S-6
// and §S-7.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	status, msg := classify(err)
	if status >= 500 {
		slog.ErrorContext(r.Context(), "request failed",
			"path", r.URL.Path,
			"method", r.Method,
			"error", err.Error(),
		)
	}
	writeJSON(w, status, errorBody{Error: msg})
}

// classify is the single source of truth for error -> HTTP status
// mapping. Anything not matched falls through to 500 and emits the
// generic "internal error" body.
//
// When adding a new sentinel error to domain or repository, add a
// case here too — otherwise it will leak as a 500.
func classify(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrInvalidIdempotencyKey),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet):
		// Validation failures are caller bugs: 400 Bad Request with
		// the domain message (safe to expose; it's user-facing).
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrIdempotencyConflict):
		// Same idempotency key, different request body. 409 is the
		// standard "your request conflicts with current state" code.
		return http.StatusConflict, err.Error()
	case errors.Is(err, repository.ErrNotFound),
		errors.Is(err, domain.ErrWalletNotFound):
		// Generic 404 with a generic message — no need to leak
		// which table the row was missing from.
		return http.StatusNotFound, "not found"
	default:
		// Everything else is an internal failure. The full error is
		// logged via writeError; the client sees only "internal error".
		return http.StatusInternalServerError, "internal error"
	}
}

// writeJSON writes a JSON response with the given status. It accepts
// any body type via json.NewEncoder; nil bodies emit only the status
// + Content-Type header (used by 204-style responses and by tests).
//
// Encode errors are deliberately swallowed — by the time we're
// writing the response there's nothing useful we can do with an
// encoder failure besides log it (and the access log middleware
// already records the status).
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}
