package helper

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sushmitaw/wallet-transfer-assignment/internal/domain"
)

type errorResponse struct {
	Error string `json:"error"`
}

func HandleError(w http.ResponseWriter, result *domain.CreateTransferResult, err error) {
	switch {
	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidIdempotencyKey):

		WriteError(w, http.StatusBadRequest, err.Error())

	case errors.Is(err, domain.ErrWalletNotFound):

		WriteError(w, http.StatusNotFound, err.Error())

	case errors.Is(err, domain.ErrIdempotencyConflict):

		WriteError(w, http.StatusConflict, err.Error())

	case errors.Is(err, domain.ErrInsufficientFunds):

		// This failure has been persisted as a FAILED transfer.
		if result != nil {
			result.Message = err.Error()
			WriteJSON(w, http.StatusUnprocessableEntity, result)
			return
		}

		WriteError(w, http.StatusUnprocessableEntity, err.Error())

	default:

		WriteError(w, http.StatusInternalServerError, "internal server error")
	}
}

func WriteError(w http.ResponseWriter, status int, message string) {
	err := errorResponse{
		Error: message,
	}
	WriteJSON(w, status, err)
}

func WriteJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
