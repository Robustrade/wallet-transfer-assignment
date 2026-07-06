package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const RequestIDKey = "RequestId"

// RequestID generates a unique ID for every incoming request
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Check if the client already sent one (e.g., from an API Gateway)

		reqID := uuid.New().String()

		// Set it in the context for our internal services to use
		c.Set(RequestIDKey, reqID)

		// Set it in the response header so the client knows it
		c.Header(RequestIDKey, reqID)

		c.Next()
	}
}
