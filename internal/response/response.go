package response

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/middleware"
)

type errorBody struct {
	Error errorDetail `json:"error"`
}

type errorDetail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Field   string `json:"field,omitempty"`
}

func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, data)
}

func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, data)
}

func ErrorWithData(c *gin.Context, status int, code, message, key string, data any) {
	c.JSON(status, gin.H{"error": errorDetail{Code: code, Message: message}, key: data})
}

func BadRequest(c *gin.Context, field, message string) {
	c.JSON(http.StatusBadRequest, errorBody{Error: errorDetail{Code: "VALIDATION_ERROR", Message: message, Field: field}})
}

func Error(c *gin.Context, err error) {
	var validationErr *domain.ValidationError
	if errors.As(err, &validationErr) {
		BadRequest(c, validationErr.Field, validationErr.Message)
		return
	}

	status, code := statusFor(err)
	message := err.Error()
	if status == http.StatusInternalServerError {
		slog.Error("internal error",
			middleware.RequestIDKey, c.GetString(middleware.RequestIDKey),
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"error", err,
		)
		message = "internal server error"
	}
	c.JSON(status, errorBody{Error: errorDetail{Code: code, Message: message}})
}

func statusFor(err error) (int, string) {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		return http.StatusNotFound, "WALLET_NOT_FOUND"
	case errors.Is(err, domain.ErrWalletAlreadyExists):
		return http.StatusConflict, "WALLET_ALREADY_EXISTS"
	case errors.Is(err, domain.ErrIdempotencyKeyReused):
		return http.StatusConflict, "IDEMPOTENCY_KEY_REUSED"
	case errors.Is(err, domain.ErrServiceBusy):
		return http.StatusServiceUnavailable, "SERVICE_BUSY"
	default:
		return http.StatusInternalServerError, "INTERNAL"
	}
}
