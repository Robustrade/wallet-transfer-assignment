package middleware

import (
	"regexp"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// HeaderRequestID carries the request id in both directions.
const HeaderRequestID = "X-Request-ID"

// Context keys that handlers set and the logger reads.
const (
	KeyRequestID      = "request_id"
	KeyIdempotencyKey = "idempotency_key"
	KeyTransferID     = "transfer_id"
	KeyOutcome        = "outcome" // processed | failed | replayed | error
	KeyError          = "error"   // internal error text, for logs only
)

// safeRequestID limits what we echo back and write to logs. Anything else
// (spaces, control characters, very long values) could be used for header or
// log injection, so it is replaced with a fresh id.
var safeRequestID = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,128}$`)

// RequestID echoes a well-formed incoming X-Request-ID, or generates a UUID.
// It stores the id in the context and sets the response header, so every
// response (including errors and panics) carries it.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if !safeRequestID.MatchString(id) {
			id = uuid.NewString()
		}
		c.Set(KeyRequestID, id)
		c.Header(HeaderRequestID, id)
		c.Next()
	}
}
