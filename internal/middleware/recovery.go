package middleware

import (
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
)

// Recovery turns a panic in any later handler into a 500 INTERNAL_ERROR JSON
// response, so one bad request cannot crash the server. The panic value and
// stack go to the logs only, never to the client. It sits after Logger, so
// the request is still logged, with status 500.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorContext(c.Request.Context(), "panic recovered",
					slog.String("request_id", c.GetString(KeyRequestID)),
					slog.String("panic", fmt.Sprint(r)),
					slog.String("stack", string(debug.Stack())))
				c.Set(KeyOutcome, "error")
				if c.Writer.Written() {
					c.Abort() // headers already sent; nothing more we can do
					return
				}
				AbortWithError(c, http.StatusInternalServerError, CodeInternal, "internal server error")
			}
		}()
		c.Next()
	}
}
