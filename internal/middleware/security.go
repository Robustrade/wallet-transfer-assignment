package middleware

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// SecurityHeaders applies defensive response headers
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Content-Security-Policy", "default-src 'none'")
		c.Next()
	}
}

// MaxBodySize limits incoming request payload sizes to prevent memory exhaustion
func MaxBodySize(limitBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limitBytes)
		}
		slog.DebugContext(c.Request.Context(), "request body limit applied", "limit_bytes", limitBytes, "path", c.Request.URL.Path)
		c.Next()
	}
}

// EnforceJSONContentType ensures write operations provide application/json
func EnforceJSONContentType() gin.HandlerFunc {
	return func(c *gin.Context) {
		method := c.Request.Method
		if method == http.MethodPost || method == http.MethodPut || method == http.MethodPatch {
			ct := c.GetHeader("Content-Type")
			if !strings.HasPrefix(ct, "application/json") {
				slog.WarnContext(c.Request.Context(), "request rejected: unsupported content type", "method", method, "path", c.Request.URL.Path, "content_type", ct)
				c.AbortWithStatusJSON(http.StatusUnsupportedMediaType, gin.H{
					"error": "Content-Type must be application/json",
				})
				return
			}
		}
		c.Next()
	}
}

// CORS configures allowed origins, methods, and specific financial headers
func CORS(allowedOrigins []string) gin.HandlerFunc {
	originMap := make(map[string]bool)
	for _, o := range allowedOrigins {
		originMap[o] = true
	}

	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")

		if originMap["*"] {
			if origin != "" {
				c.Header("Access-Control-Allow-Origin", origin)
				c.Writer.Header().Add("Vary", "Origin")
			} else {
				c.Header("Access-Control-Allow-Origin", "*")
			}
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, Authorization")
			c.Header("Access-Control-Max-Age", "86400")
		} else if origin != "" && originMap[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Add("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, POST, PATCH, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, Idempotency-Key, Authorization")
			c.Header("Access-Control-Max-Age", "86400")
		}

		if c.Request.Method == http.MethodOptions {
			slog.DebugContext(c.Request.Context(), "cors preflight request handled", "origin", origin, "path", c.Request.URL.Path)
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}
