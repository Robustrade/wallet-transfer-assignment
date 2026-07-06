package middleware

import (
	"github.com/gin-gonic/gin"
)

// Recovery recovers from any panics and writes a 500 if there was one.
func Recovery() gin.HandlerFunc {
	return gin.Recovery()
}
