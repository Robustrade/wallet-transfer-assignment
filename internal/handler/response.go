package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/synchone/wallet-transfer-assignment/internal/domain"
)

func writeJSON(c *gin.Context, status int, payload any) {
	c.JSON(status, payload)
}

func writeError(c *gin.Context, status int, message string) {
	writeJSON(c, status, gin.H{"error": message})
}

func writeDomainError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, domain.ErrMissingIdempotency),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidRequest):
		writeError(c, http.StatusBadRequest, err.Error())
	case errors.Is(err, domain.ErrWalletNotFound):
		writeError(c, http.StatusNotFound, err.Error())
	default:
		var validation domain.ValidationError
		if errors.As(err, &validation) {
			writeError(c, http.StatusBadRequest, validation.Error())
			return
		}
		writeError(c, http.StatusInternalServerError, "internal server error")
	}
}
