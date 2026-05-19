package httpapi

import (
	"errors"
	"net/http"

	"github.com/hariharandr/wallet-transfer-assignment/internal/apperr"
	"github.com/hariharandr/wallet-transfer-assignment/internal/domain"
)

type Response struct {
	TransferID string `json:"transferId,omitempty"`
	Status     string `json:"status,omitempty"`
	FromWallet string `json:"fromWalletId,omitempty"`
	ToWallet   string `json:"toWalletId,omitempty"`
	Amount     int64  `json:"amount,omitempty"`
	Error      string `json:"error,omitempty"`
}

func StatusFor(err error) int {
	switch {
	case errors.Is(err, apperr.ErrInvalidRequest),
		errors.Is(err, apperr.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount):
		return http.StatusBadRequest
	case errors.Is(err, apperr.ErrWalletNotFound):
		return http.StatusNotFound
	case errors.Is(err, apperr.ErrInProgress):
		return http.StatusConflict
	case errors.Is(err, apperr.ErrKeyReused),
		errors.Is(err, apperr.ErrInsufficientFunds):
		return http.StatusUnprocessableEntity
	default:
		return http.StatusInternalServerError
	}
}

func WriteError(w http.ResponseWriter, status int, msg string) {
	WriteJSON(w, status, Response{Error: msg})
}
