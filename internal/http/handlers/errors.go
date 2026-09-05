package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"wallet-transfer/internal/domain"
)

type ErrorCode string

const (
	CodeValidation        ErrorCode = "VALIDATION_ERROR"
	CodeWalletNotFound    ErrorCode = "WALLET_NOT_FOUND"
	CodeTransferNotFound  ErrorCode = "TRANSFER_NOT_FOUND"
	CodeInsufficient      ErrorCode = "INSUFFICIENT_FUNDS"
	CodeCurrencyMismatch  ErrorCode = "CURRENCY_MISMATCH"
	CodeKeyConflict       ErrorCode = "IDEMPOTENCY_KEY_CONFLICT"
	CodeConflict          ErrorCode = "CONFLICT"
	CodeRetryableConflict ErrorCode = "RETRYABLE_CONFLICT"
	CodeTimeout           ErrorCode = "REQUEST_TIMEOUT"
	CodeCanceled          ErrorCode = "REQUEST_CANCELED"
	CodeInternal          ErrorCode = "INTERNAL_ERROR"
)

type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

type ErrorDetail struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
	Field   string    `json:"field,omitempty"`
}

func RespondError(c *gin.Context, err error) {
	status, detail := classify(err)

	if status >= http.StatusInternalServerError {
		slog.Error("request failed",
			"error", err,
			"path", c.FullPath(),
			"status", status,
		)
	}

	c.AbortWithStatusJSON(status, ErrorBody{Error: detail})
}

type errorRule struct {
	target  error
	status  int
	code    ErrorCode
	message string
}

var errorRules = []errorRule{
	{domain.ErrValidation, http.StatusBadRequest, CodeValidation, ""},
	{domain.ErrWalletNotFound, http.StatusNotFound, CodeWalletNotFound, ""},
	{domain.ErrTransferNotFound, http.StatusNotFound, CodeTransferNotFound, ""},
	{domain.ErrIdempotencyKeyConflict, http.StatusConflict, CodeKeyConflict, ""},
	{domain.ErrIdempotencyKeyAlreadyUsed, http.StatusConflict, CodeConflict, "a concurrent request is using this idempotency key; retry"},
	{domain.ErrInvalidStateTransition, http.StatusConflict, CodeConflict, ""},
	{domain.ErrConflict, http.StatusConflict, CodeRetryableConflict, "the transfer conflicted with a concurrent one; retry the same request"},
	{domain.ErrInsufficientFunds, http.StatusUnprocessableEntity, CodeInsufficient, "the source wallet does not have sufficient funds for this transfer"},
	{domain.ErrCurrencyMismatch, http.StatusUnprocessableEntity, CodeCurrencyMismatch, ""},
	{domain.ErrTimeout, http.StatusGatewayTimeout, CodeTimeout, "the request exceeded its deadline and was rolled back"},
	{context.DeadlineExceeded, http.StatusGatewayTimeout, CodeTimeout, "the request exceeded its deadline and was rolled back"},
	{context.Canceled, statusClientClosedRequest, CodeCanceled, "the request was canceled"},
}

const statusClientClosedRequest = 499

func classify(err error) (int, ErrorDetail) {
	var validationErr *domain.ValidationError
	if errors.As(err, &validationErr) {
		return http.StatusBadRequest, ErrorDetail{
			Code:    CodeValidation,
			Message: validationErr.Message,
			Field:   validationErr.Field,
		}
	}

	for _, rule := range errorRules {
		if !errors.Is(err, rule.target) {
			continue
		}
		message := rule.message
		if message == "" {
			message = err.Error()
		}
		return rule.status, ErrorDetail{Code: rule.code, Message: message}
	}

	return http.StatusInternalServerError, ErrorDetail{
		Code:    CodeInternal,
		Message: "an unexpected error occurred",
	}
}
