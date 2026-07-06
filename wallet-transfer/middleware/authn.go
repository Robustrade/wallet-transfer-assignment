package middleware

import (
	"github.com/gin-gonic/gin"
)

// Authentication middleware.
// This is a placeholder for actual authentication logic.
// Authentication here relies on an organization or external authentication provider.
func Authentication() gin.HandlerFunc {
	return func(c *gin.Context) {
		// External authentication logic would go here (e.g., verifying a JWT from an Identity Provider).
		// This inlcudes OKTA, GOOGLEAPI, PKI
		// For now, we skip it and proceed to the next handler.
		c.Next()
	}
}
