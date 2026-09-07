package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"Wallet_Transfer_Service/internal/domain"
)

type errorResponse struct {
	Error string `json:"error"`
}

// statusForError maps errors to HTTP status codes.
func statusForError(err error) int {
	switch {
	case errors.Is(err, domain.ErrInvalidRequest),
		errors.Is(err, domain.ErrInvalidWalletID),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrNegativeBalance):
		return http.StatusBadRequest
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		return http.StatusUnprocessableEntity
	case errors.Is(err, domain.ErrIdempotencyKeyInUse):
		return http.StatusConflict
	case errors.Is(err, domain.ErrWalletNotFound), errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound
	case errors.Is(err, domain.ErrWalletExists):
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, err error) {
	status := statusForError(err)
	if status == http.StatusInternalServerError {
		log.Printf("internal error: %v", err)
		writeJSON(w, status, errorResponse{Error: "internal server error"})
		return
	}
	writeJSON(w, status, errorResponse{Error: err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(body); err != nil {
		log.Printf("write response: %v", err)
	}
}
