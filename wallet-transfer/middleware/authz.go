package middleware

import (
	"github.com/gin-gonic/gin"
)

// Authorization middleware.
// This is a placeholder for actual authorization logic.
// Authorization can be internal, evaluating roles or policies for the endpoint.
func Authorization() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Internal authorization logic would go here (e.g., checking if the user has transfer permissions).
		// For now, we skip it and proceed to the next handler.
		c.Next()
	}
}
