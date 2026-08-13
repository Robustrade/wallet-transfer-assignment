package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/robustrade/wallet-transfer-assignment/internal/handler"
)

func New(transferHandler *handler.TransferHandler, walletHandler *handler.WalletHandler) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery(), requestLogger())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	r.POST("/transfers", transferHandler.Create)
	r.GET("/transfers/:id", transferHandler.Get)

	r.POST("/wallets", walletHandler.Create)
	r.GET("/wallets/:id", walletHandler.Get)
	r.GET("/wallets/:id/ledger", walletHandler.Ledger)

	return r
}

// requestLogger is a minimal structured-logging middleware: method, path,
// status and latency for every request, satisfying the assignment's
// observability expectation without pulling in a metrics/tracing stack.
func requestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path

		c.Next()

		slog.Info("request",
			"method", c.Request.Method,
			"path", path,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(start).Milliseconds(),
		)
	}
}
