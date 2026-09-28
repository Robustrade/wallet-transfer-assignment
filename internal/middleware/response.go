// Package middleware contains the Gin middleware chain and the JSON error
// envelope shared by every error response.
package middleware

import "github.com/gin-gonic/gin"

// Error codes in the API contract.
const (
	CodeValidation      = "VALIDATION_ERROR"
	CodePayloadTooLarge = "PAYLOAD_TOO_LARGE"
	CodeWalletNotFound  = "WALLET_NOT_FOUND"
	CodeKeyReused       = "IDEMPOTENCY_KEY_REUSED"
	CodeUnavailable     = "TEMPORARILY_UNAVAILABLE"
	CodeInternal        = "INTERNAL_ERROR"
	CodeNotFound        = "NOT_FOUND" // unknown route
)

// ErrorBody is the JSON shape of every error response:
//
//	{ "error": { "code": "...", "message": "...", "requestId": "..." } }
type ErrorBody struct {
	Error ErrorDetail `json:"error"`
}

// ErrorDetail is the inner object of ErrorBody.
type ErrorDetail struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	RequestID string `json:"requestId"`
}

// AbortWithError writes the error envelope and stops the handler chain.
// message must be safe to show to clients: never pass raw internal error
// text.
func AbortWithError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, ErrorBody{Error: ErrorDetail{
		Code:      code,
		Message:   message,
		RequestID: c.GetString(KeyRequestID),
	}})
}
