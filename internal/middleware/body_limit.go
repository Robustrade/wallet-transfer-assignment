package middleware

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// BodyLimit rejects request bodies larger than maxBytes with 413.
//   - If Content-Length is declared and too large, it rejects immediately,
//     without reading anything.
//   - Otherwise (unknown length, or a client that lies), it wraps the body in
//     http.MaxBytesReader, so reading past the limit fails with
//     *http.MaxBytesError. The handler maps that error to 413.
func BodyLimit(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			AbortWithError(c, http.StatusRequestEntityTooLarge, CodePayloadTooLarge,
				fmt.Sprintf("request body must not exceed %d bytes", maxBytes))
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		c.Next()
	}
}
