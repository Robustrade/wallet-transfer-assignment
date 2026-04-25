package main

import (
	"log"
	"os"

	"github.com/gin-gonic/gin"
	_ "github.com/lib/pq"
	"github.com/robustrade/wallet-transfer-assignment/config"
	"github.com/robustrade/wallet-transfer-assignment/internal/db"
	"github.com/robustrade/wallet-transfer-assignment/internal/handler"
	"github.com/robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

func main() {
	configPath := os.Getenv("CONFIG_PATH")
	if configPath == "" {

		configPath = "internal/config/config.yaml"

	}

	cfg, err := config.LoadConfig()
	if err != nil {

		log.Fatalf("failed to load config: %v", err)

	}

	db, err := db.NewPostgresDB(cfg.Database)
	if err != nil {
		log.Fatalf("failed to connect DB: %v", err)
	}
	defer db.Close()

	walletRepo := repository.NewWalletRepository(db)
	walletService := service.NewTransferService(walletRepo)
	walletHandler := handler.NewTransferHandler(walletService)

	r := gin.Default()

	r.POST("/transfer", walletHandler.Transfer)

	log.Printf("server running on port %s", cfg.Server.Port)
	if err := r.Run(":" + cfg.Server.Port); err != nil {
		log.Fatalf("failed to start server: %v", err)
	}
}
