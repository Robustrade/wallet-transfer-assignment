package handler

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/synchone/wallet-transfer-assignment/internal/observability"
	"github.com/synchone/wallet-transfer-assignment/internal/service"
)

type Dependencies struct {
	TransferService *service.TransferService
	HealthPinger    Pinger
	WalletBalances  WalletBalanceReader
	Logger          *slog.Logger
}

func NewRouter(deps Dependencies) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(observability.RequestID())
	r.Use(observability.RequestLogger(deps.Logger))

	transfer := NewTransferHandler(deps.TransferService, deps.Logger)
	health := NewHealthHandler(deps.HealthPinger)
	wallet := NewWalletHandler(deps.WalletBalances)

	r.GET("/health", health.Health)
	r.GET("/wallets/:id/balance", wallet.GetBalance)
	r.POST("/transfers", transfer.CreateTransfer)

	return r
}
