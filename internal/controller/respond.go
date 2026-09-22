// Package controller implements HTTP handlers: request decoding, response
// encoding, and translating domain/service errors into HTTP status codes.
// It contains no business logic — that lives entirely in the service
// package.
package controller

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/models"
)

// WriteJSON encodes v as the response body with the given status code.
func WriteJSON(w http.ResponseWriter, logger *slog.Logger, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("encode response", "error", err)
	}
}

// WriteError writes a structured error body. code is a short machine
// readable identifier (e.g. "insufficient_funds") for API consumers.
func WriteError(w http.ResponseWriter, logger *slog.Logger, status int, code, message string) {
	WriteJSON(w, logger, status, models.ErrorResponse{Error: message, Code: code})
}
