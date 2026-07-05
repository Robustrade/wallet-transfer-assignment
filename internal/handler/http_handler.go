// Package handler is the transport layer: decode the request, call the
// service, encode the response. No business logic lives here.
package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/service"
)

type TransferHandler struct {
	svc *service.TransferService
}

func NewTransferHandler(svc *service.TransferService) *TransferHandler {
	return &TransferHandler{svc: svc}
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(w http.ResponseWriter, r *http.Request) {
	var req service.CreateTransferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	result, err := h.svc.CreateTransfer(r.Context(), req)
	if err != nil {
		writeServiceError(w, err)
		return
	}

	// A FAILED transfer is still a well-formed, successfully-processed
	// request — the business outcome just wasn't a success. 422 signals
	// that distinction rather than a blanket 200 or a 5xx.
	status := http.StatusCreated
	if result.State == domain.TransferFailed {
		status = http.StatusUnprocessableEntity
	}
	writeJSON(w, status, result)
}

// writeServiceError maps domain sentinel errors to HTTP status codes.
func writeServiceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrIdempotencyKeyRequired),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrWalletNotFound):
		writeError(w, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrDuplicateInFlight):
		// 409 rather than 425 for broader client compatibility; the message
		// tells the caller it's safe (and expected) to retry.
		writeError(w, http.StatusConflict, err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal server error")
	}
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}
