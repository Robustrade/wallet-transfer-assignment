package app

import (
	"database/sql"
	transferhandler "wallet-transfer/internal/transfer/handler"
	transferrepository "wallet-transfer/internal/transfer/repository"
	transferservice "wallet-transfer/internal/transfer/service"
	wallethandler "wallet-transfer/internal/wallet/handler"
	walletrepository "wallet-transfer/internal/wallet/repository"
	walletservice "wallet-transfer/internal/wallet/service"

	"github.com/gin-gonic/gin"
)

func registerRoutes(router *gin.Engine, db *sql.DB) {
	wr := walletrepository.New(db)
	tr := transferrepository.New(db)
	wh := wallethandler.New(walletservice.New(wr))
	th := transferhandler.New(transferservice.New(tr))
	v1 := router.Group("/api/v1")
	v1.GET("/wallets", wh.List)
	v1.POST("/wallets", wh.Create)
	v1.GET("/wallets/:id", wh.Get)
	v1.POST("/wallets/:id/balance", wh.UpdateBalance)
	v1.GET("/wallets/:id/balance", wh.Balance)
	v1.GET("/wallets/:id/balance/history", wh.BalanceHistory)
	v1.GET("/wallets/:id/transactions", wh.Transfers)
	v1.POST("/transfers", th.Create)
	v1.GET("/transfers/:id", th.Get)
}
