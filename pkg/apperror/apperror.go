package apperror

import (
	"fmt"

	"github.com/gofiber/fiber/v2"
)

// AppError is a structured application error with HTTP status code.
type AppError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *AppError) Error() string {
	return fmt.Sprintf("(%d) %s", e.Code, e.Message)
}

func New(code int, message string) *AppError {
	return &AppError{Code: code, Message: message}
}

func NotFound(message string) *AppError {
	return New(fiber.StatusNotFound, message)
}

func BadRequest(message string) *AppError {
	return New(fiber.StatusBadRequest, message)
}

func Unauthorized(message string) *AppError {
	return New(fiber.StatusUnauthorized, message)
}

func Conflict(message string) *AppError {
	return New(fiber.StatusConflict, message)
}

func InternalServerError(message string) *AppError {
	return New(fiber.StatusInternalServerError, message)
}
