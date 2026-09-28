package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/middleware"
	"github.com/LokilokeshM/wallet-transfer-assignment/internal/service"
)

// respondError maps an error to its HTTP status, code and a client-safe
// message. The raw error text goes to the request log only. 5xx responses
// always use a fixed message, so database or driver details never leak.
func respondError(c *gin.Context, err error) {
	status, code, message := classify(err)
	c.Set(middleware.KeyOutcome, "error")
	c.Set(middleware.KeyError, err.Error())
	middleware.AbortWithError(c, status, code, message)
}

func classify(err error) (status int, code, message string) {
	var ve *service.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, middleware.CodeValidation, ve.Error()
	case errors.Is(err, domain.ErrValidation):
		return http.StatusBadRequest, middleware.CodeValidation, "invalid request"
	case errors.Is(err, errPayloadTooLarge):
		return http.StatusRequestEntityTooLarge, middleware.CodePayloadTooLarge, "request body is too large"
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound, middleware.CodeWalletNotFound, "wallet not found"
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		return http.StatusConflict, middleware.CodeKeyReused,
			"idempotencyKey was already used with a different request"
	case errors.Is(err, service.ErrUnavailable),
		errors.Is(err, context.DeadlineExceeded),
		errors.Is(err, context.Canceled):
		return http.StatusServiceUnavailable, middleware.CodeUnavailable,
			"the service is temporarily unavailable; retry with the same idempotencyKey"
	default:
		return http.StatusInternalServerError, middleware.CodeInternal, "internal server error"
	}
}

// errUnexpectedStatus means the service returned a transfer that is not in a
// final state. That would be a bug, so it is reported as a 500.
func errUnexpectedStatus(s domain.Status) error {
	return fmt.Errorf("service returned transfer in non-final status %q", s)
}
