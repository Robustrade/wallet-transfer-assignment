package http

import (
	"context"
	"log/slog"
	nethttp "net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"wallet-transfer/internal/http/handlers"
	"wallet-transfer/internal/http/middlewares"
	"wallet-transfer/internal/metrics"
)

type Health interface {
	Ping(ctx context.Context) error
}

type RouterConfig struct {
	Transfers      handlers.TransferService
	Wallets        handlers.WalletService
	Health         Health
	Metrics        *metrics.Metrics
	Registry       *prometheus.Registry
	RequestTimeout time.Duration
}

func NewRouter(cfg RouterConfig) *gin.Engine {
	engine := gin.New()

	engine.Use(
		middlewares.RequestContext(),
		middlewares.RequestMetrics(cfg.Metrics),
		middlewares.RequestLogging(),
		middlewares.Recovery(),
		middlewares.Timeout(cfg.RequestTimeout),
	)

	engine.NoRoute(func(c *gin.Context) {
		c.JSON(nethttp.StatusNotFound, gin.H{
			"error": gin.H{
				"code":    "NOT_FOUND",
				"message": "no such endpoint",
			},
		})
	})

	transfers := handlers.NewTransferHandler(cfg.Transfers, cfg.Metrics)
	wallets := handlers.NewWalletHandler(cfg.Wallets)

	engine.POST("/transfers", transfers.Create)
	engine.GET("/transfers/:id", transfers.Get)
	engine.GET("/wallets/:id", wallets.Get)
	engine.GET("/wallets/:id/transfers", wallets.GetHistory)

	engine.GET("/healthz", func(c *gin.Context) {
		c.JSON(nethttp.StatusOK, gin.H{"status": "ok"})
	})

	engine.GET("/readyz", func(c *gin.Context) {
		if cfg.Health != nil {
			if err := cfg.Health.Ping(c.Request.Context()); err != nil {
				slog.Warn("readiness check failed", "error", err)
				c.JSON(nethttp.StatusServiceUnavailable, gin.H{"status": "unavailable", "reason": "database"})
				return
			}
		}
		c.JSON(nethttp.StatusOK, gin.H{"status": "ready"})
	})

	if cfg.Registry != nil {
		engine.GET("/metrics", gin.WrapH(promhttp.HandlerFor(cfg.Registry, promhttp.HandlerOpts{})))
	}

	return engine
}
