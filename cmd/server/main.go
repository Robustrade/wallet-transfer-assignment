package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/Robustrade/wallet-transfer-assignment/internal/config"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository/postgres"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	if err := postgres.Migrate(cfg.DB.URL()); err != nil {
		log.Fatalf("migrate database: %v", err)
	}

	db, err := postgres.Connect(cfg.DB.URL())
	if err != nil {
		log.Fatalf("connect database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("get sql.DB: %v", err)
	}

	router := gin.New()
	router.Use(gin.Recovery())

	router.GET("/healthz", func(c *gin.Context) {
		if err := sqlDB.PingContext(c.Request.Context()); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "database unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	log.Printf("listening on :%s", cfg.Port)
	if err := router.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
