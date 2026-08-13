package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

func writeError(c *gin.Context, err error) {
	status, message := mapError(err)
	c.JSON(status, ErrorResponse{Error: message})
}

func mapError(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound), errors.Is(err, domain.ErrTransferNotFound):
		return http.StatusNotFound, err.Error()
	case errors.Is(err, domain.ErrWalletAlreadyExists):
		return http.StatusConflict, err.Error()
	case errors.Is(err, domain.ErrSameWallet), errors.Is(err, domain.ErrInvalidAmount), errors.Is(err, domain.ErrCurrencyMismatch), errors.Is(err, domain.ErrMissingIdempotencyKey):
		return http.StatusBadRequest, err.Error()
	case errors.Is(err, domain.ErrIdempotencyKeyReused), errors.Is(err, domain.ErrRequestInProgress):
		return http.StatusConflict, err.Error()
	default:
		return http.StatusInternalServerError, "internal server error"
	}
}
