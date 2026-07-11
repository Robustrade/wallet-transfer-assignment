// Package middleware holds cross-cutting HTTP concerns. Attached per
// route-group, not globally — there's currently no concern that applies
// to literally every route (e.g. /health deliberately stays
// dependency-free).
package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/logger"
)

const correlationIDHeader = "X-Correlation-ID"

// CorrelationID attaches a per-request correlation ID to the request
// context, reusing an inbound X-Correlation-ID header if present and
// echoing it back on the response. This is the one sanctioned use of
// context.Value here: request-scoped data only, never app-scoped
// dependencies like a DB pool, which are constructor-injected instead.
//
// The context key itself lives in internal/platform/logger, not here —
// that's the only way a service or handler can read the correlation ID
// back out for logging without depending on Gin.
func CorrelationID() gin.HandlerFunc {
	return func(c *gin.Context) {
		correlationID := c.GetHeader(correlationIDHeader)
		if correlationID == "" {
			correlationID = uuid.NewString()
		}

		ctx := logger.WithCorrelationID(c.Request.Context(), correlationID)
		c.Request = c.Request.WithContext(ctx)

		c.Header(correlationIDHeader, correlationID)
		c.Next()
	}
}
