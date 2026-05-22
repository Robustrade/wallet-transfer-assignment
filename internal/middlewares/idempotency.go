package middlewares

import (
	"bytes"
	"net/http"

	"wallet-transfer-assignment/internal/services/cache"
	"wallet-transfer-assignment/internal/utils"

	"github.com/gin-gonic/gin"
)

// responseBodyWriter intercepts the response so we can store it in the cache.
type responseBodyWriter struct {
	gin.ResponseWriter
	body *bytes.Buffer
}

func (w responseBodyWriter) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

func (w *responseBodyWriter) WriteString(s string) (int, error) {
	w.body.WriteString(s)
	return w.ResponseWriter.WriteString(s)
}

// IdempotencyMiddleware ensures "Exactly-Once" semantics for transfer requests.
// It implements a Tier 1 (In-Memory) safety layer that:
// 1. Blocks duplicate sequential requests (returns cached result).
// 2. Blocks duplicate concurrent requests (returns 409 Conflict).
// 3. Allows retries on server errors (Rollback on 5xx).
func IdempotencyMiddleware(cacheService cache.CacheHelper) gin.HandlerFunc {
	return func(c *gin.Context) {
		// Only apply to POST requests as per REST best practices for idempotency.
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		// Requirement: Idempotency-Key header is mandatory for all transfers.
		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			utils.GeneralAPIValidationError(c, "Idempotency-Key header is required")
			c.Abort()
			return
		}

		// Retrieve the state of this specific key from Tier 1 cache.
		item, ok := cacheService.Get(key)
		if !ok {
			// A key MUST be generated via /generate-id before use.
			utils.GeneralAPIValidationError(c, "Invalid or unissued Idempotency-Key")
			c.Abort()
			return
		}

		switch item.State {
		case cache.StateIssued:
			// ATOMIC TRANSITION: Transition from ISSUED -> IN_FLIGHT.
			// This CompareAndSwap is the single point of truth that prevents race conditions.
			if !cacheService.CompareAndSwapState(key, cache.StateIssued, cache.StateInFlight) {
				// If swap fails, another goroutine won the race.
				utils.GeneralAPIConflictError(c, "Request already in-flight")
				c.Abort()
				return
			}

			// Wrap the writer to intercept the downstream response for caching.
			rbw := &responseBodyWriter{body: bytes.NewBufferString(""), ResponseWriter: c.Writer}
			c.Writer = rbw

			// Execute the business logic (Controller/Repository).
			c.Next()

			// Post-processing: Finalize the cache state based on the HTTP status code.
			statusCode := c.Writer.Status()

			// Refresh item reference to ensure we update the latest cache snapshot.
			item, _ = cacheService.Get(key)

			if statusCode >= 500 {
				// ROLLBACK: If a system error occurred, reset state to ISSUED.
				// This allows the client to retry the SAME key for a legitimate recovery attempt.
				item.State = cache.StateIssued
				item.StatusCode = 0
				item.Response = nil
				cacheService.Set(key, item)
			} else {
				// COMMIT: Success (2xx) or Client/Business Failure (4xx) are permanent results.
				// We cache these responses to prevent re-execution of the business logic.
				item.State = cache.StateCompleted
				item.StatusCode = statusCode
				item.Response = rbw.body.Bytes()
				cacheService.Set(key, item)
			}

		case cache.StateInFlight:
			// Sequential hit while the first request is still processing.
			utils.GeneralAPIConflictError(c, "Request already in-flight")
			c.Abort()

		case cache.StateCompleted:
			// Sequential hit after processing is finished.
			// Serve the exact same response as the first time.
			c.Data(item.StatusCode, "application/json", item.Response)
			c.Abort()

		default:
			// Defensive handling for unexpected cache state values.
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{
				"error": "Unknown idempotency state",
			})
		}
	}
}
