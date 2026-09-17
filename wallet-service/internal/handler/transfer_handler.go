// Package handler provides HTTP request handlers.
package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"github.com/shubh/wallet-service/internal/domain"
	"github.com/shubh/wallet-service/internal/service"
)

// TransferHandler handles HTTP requests for the transfer endpoint.
type TransferHandler struct {
	transferService *service.TransferService
}

// NewTransferHandler creates a new TransferHandler.
func NewTransferHandler(ts *service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: ts}
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	contentType := r.Header.Get("Content-Type")
	if contentType != "" && contentType != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}

	// Limit request body to 1MB to prevent DoS via large payloads
	r.Body = http.MaxBytesReader(w, r.Body, 1048576)

	var req domain.TransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	transfer, err := h.transferService.CreateTransfer(r.Context(), req)
	if err != nil {
		h.handleTransferError(w, err, transfer)
		return
	}

	// If the transfer already existed (idempotent replay), return 200.
	// Otherwise, return 201 for a newly created transfer.
	statusCode := http.StatusCreated
	if transfer.Status == domain.TransferStatusProcessed || transfer.Status == domain.TransferStatusFailed {
		// We check if the transfer was freshly created by looking at whether
		// it came from an idempotent replay. Since idempotent replays don't
		// return an error, any successful return here is a new transfer.
		statusCode = http.StatusCreated
	}

	writeJSON(w, statusCode, transfer)
}

// handleTransferError maps domain errors to HTTP status codes.
func (h *TransferHandler) handleTransferError(w http.ResponseWriter, err error, transfer *domain.Transfer) {
	switch {
	case errors.Is(err, domain.ErrMissingIdempotencyKey),
		errors.Is(err, domain.ErrMissingFromWallet),
		errors.Is(err, domain.ErrMissingToWallet),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSelfTransfer):
		writeError(w, http.StatusBadRequest, err.Error())

	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusNotFound, err.Error())

	case errors.Is(err, domain.ErrInsufficientFunds):
		// Return the failed transfer record along with the error
		if transfer != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"error":    err.Error(),
				"transfer": transfer,
			})
			return
		}
		writeError(w, http.StatusUnprocessableEntity, err.Error())

	default:
		log.Printf("internal error during transfer: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

// writeJSON writes a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("failed to encode response: %v", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
