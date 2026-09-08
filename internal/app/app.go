package app

import (
	"context"
	"database/sql"
	"github.com/gin-gonic/gin"
	"wallet-transfer/internal/platform/config"
	"wallet-transfer/internal/platform/database"
	"wallet-transfer/internal/platform/httpserver"
)

type App struct {
	cfg config.Config
	db  *sql.DB
}

func New(ctx context.Context, cfg config.Config) (*App, error) {
	db, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, err
	}
	return &App{cfg: cfg, db: db}, nil
}
func (a *App) Run() error {
	defer a.db.Close()
	router := httpserver.New()
	router.GET("/health", func(c *gin.Context) {
		if err := database.Healthy(a.db); err != nil {
			c.JSON(503, gin.H{"status": "unhealthy"})
			return
		}
		c.JSON(200, gin.H{"status": "ok"})
	})
	registerRoutes(router, a.db)
	return router.Run(":" + a.cfg.HTTPPort)
}
