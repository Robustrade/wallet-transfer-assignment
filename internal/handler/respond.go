// Package handler is the HTTP layer: decode and validate requests, call the
// services, map results and errors to HTTP. It holds no business rules.
package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

const maxBodyBytes = 1 << 20

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// errorMapping maps domain errors to HTTP status and a stable error code.
var errorMapping = []struct {
	err    error
	status int
	code   string
}{
	{domain.ErrValidation, http.StatusBadRequest, "VALIDATION_ERROR"},
	{domain.ErrWalletNotFound, http.StatusNotFound, "WALLET_NOT_FOUND"},
	{domain.ErrTransferNotFound, http.StatusNotFound, "TRANSFER_NOT_FOUND"},
	{domain.ErrIdempotencyKeyReused, http.StatusConflict, "IDEMPOTENCY_KEY_REUSED"},
	{domain.ErrWalletAlreadyExists, http.StatusConflict, "WALLET_ALREADY_EXISTS"},
	{domain.ErrInvalidStateTransition, http.StatusConflict, "INVALID_STATE_TRANSITION"},
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	var body errorBody
	for _, m := range errorMapping {
		if errors.Is(err, m.err) {
			body.Error.Code, body.Error.Message = m.code, err.Error()
			writeJSON(w, m.status, body)
			return
		}
	}
	log.ErrorContext(r.Context(), "unhandled error", "method", r.Method, "path", r.URL.Path, "error", err)
	body.Error.Code = "INTERNAL_ERROR"
	body.Error.Message = "internal error, safe to retry with the same idempotency key"
	writeJSON(w, http.StatusInternalServerError, body)
}

// decodeJSON reads a strict JSON body: unknown fields, wrong types (e.g. "100" or 10.5
// for an integer) and trailing data are all validation errors.
func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(http.MaxBytesReader(nil, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("%w: invalid JSON body: %w", domain.ErrValidation, err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return fmt.Errorf("%w: body must contain a single JSON object", domain.ErrValidation)
	}
	return nil
}
