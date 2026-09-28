package middleware

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
)

// Timeout puts a deadline on the request context. The service and database
// calls observe it: a transaction still running at the deadline is rolled
// back, and the handler answers 503.
//
// It only sets the deadline. It does not write a response from a separate
// goroutine, unlike some timeout middleware, which can race with the handler
// writing its own response.
func Timeout(d time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), d)
		defer cancel()
		c.Request = c.Request.WithContext(ctx)
		c.Next()
	}
}
