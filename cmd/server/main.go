package main

import (
	"github.com/gin-gonic/gin"
	"github.com/trjade/wallet-transfer-service/internal/config"
	"github.com/trjade/wallet-transfer-service/internal/db"
	"github.com/trjade/wallet-transfer-service/internal/handler"
	"github.com/trjade/wallet-transfer-service/internal/logger"
	"github.com/trjade/wallet-transfer-service/internal/repository/postgres"
	"github.com/trjade/wallet-transfer-service/internal/service"
	"go.uber.org/zap"
)

func main() {
	cnfg := config.Load()

	log, err := logger.New()
	if err != nil {
		panic(err)
	}

	dbPool, err := db.NewPostgresDB(cnfg.DBURL)
	if err != nil {
		panic(err)
	}
	defer dbPool.Close()

	if err := db.RunMigrations(cnfg.DBURL); err != nil {
		log.Fatal("Failed to run database migrations", zap.Error(err))
	}
	log.Info("database migrations applied")

	walletRepo := postgres.NewWalletRepository(dbPool)
	transferRepo := postgres.NewTransferRepository(dbPool)
	ledgerRepo := postgres.NewLedgerRepository(dbPool)
	txManager := postgres.NewTxManager(dbPool)

	transferService := service.NewTransferService(txManager,
		walletRepo,
		transferRepo,
		ledgerRepo,
	)

	transferHandler := handler.NewTransferHandler(transferService)

	gin.SetMode(gin.ReleaseMode)
	r := gin.Default()

	r.POST(
		"/transfers",
		transferHandler.CreateTransfer,
	)

	// Initialize and start the server here
	log.Info("Server is starting", zap.String("port", cnfg.Port))

	if err := r.Run(":" + cnfg.Port); err != nil {
		log.Fatal("Failed to start server", zap.Error(err))
	}
}
