package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

// ErrorResponse represents the standard JSON error structure.
type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail describes the error code and message.
type ErrorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// WriteJSON sends a JSON response with the given status code and payload.
func WriteJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if data != nil {
		if err := json.NewEncoder(w).Encode(data); err != nil {
			log.Printf("failed to write JSON response: %v", err)
		}
	}
}

// WriteError maps domain errors or general errors to the appropriate HTTP status code and JSON response.
func WriteError(w http.ResponseWriter, err error) {
	var de *domain.DomainError
	if errors.As(err, &de) {
		status := http.StatusBadRequest
		switch de.Code {
		case domain.CodeWalletNotFound, domain.CodeTransferNotFound:
			status = http.StatusNotFound
		case domain.CodeInsufficientFunds, domain.CodeIdempotencyKeyReuseMismatch:
			status = http.StatusConflict
		case domain.CodeInvalidRequest, domain.CodeInvalidAmount, domain.CodeSameWalletTransfer:
			status = http.StatusBadRequest
		default:
			status = http.StatusBadRequest
		}

		WriteJSON(w, status, ErrorResponse{
			Error: ErrorDetail{
				Code:    de.Code,
				Message: de.Message,
			},
		})
		return
	}

	// Unexpected system/technical error: do not leak raw database errors or stack traces to clients
	log.Printf("Internal server error: %v", err)
	WriteJSON(w, http.StatusInternalServerError, ErrorResponse{
		Error: ErrorDetail{
			Code:    domain.CodeInternalError,
			Message: "An unexpected technical error occurred. Please try again later.",
		},
	})
}
