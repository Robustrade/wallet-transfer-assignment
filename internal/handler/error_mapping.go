package handler

import (
	"errors"
	"net/http"

	"github.com/robustrade/wallet-transfer-assignment/internal/model"
)

func mapErrorToStatus(err error) (int, string) {
	switch {
	case errors.Is(err, model.ErrInvalidAmount):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, model.ErrMissingWalletID):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, model.ErrSameWallet):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, model.ErrMissingIdempotencyKey):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, model.ErrWalletNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, model.ErrTransferFailedError):
		return http.StatusUnprocessableEntity, err.Error()
	default:
		return http.StatusInternalServerError, err.Error()
	}
}
