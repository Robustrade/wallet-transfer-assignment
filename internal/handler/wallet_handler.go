package handler

import (
	"net/http"
	"wallet-transfer-service/internal/service"

	"github.com/gin-gonic/gin"
)

type WalletHandler struct {
	walletService *service.WalletService
}

func NewWalletHandler(walletService *service.WalletService) *WalletHandler {
	return &WalletHandler{
		walletService: walletService,
	}
}

func (h *WalletHandler) GetWallet(c *gin.Context) {

	id := c.Param("id")

	wallet, err := h.walletService.GetWallet(
		c.Request.Context(),
		id,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, wallet)
}

func (h *WalletHandler) CreateTransfer(c *gin.Context) {

	var req CreateTransferRequest

	err := c.ShouldBindJSON(&req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	err = h.walletService.CreateTransfer(req.FromWalletID, req.ToWalletID, req.IdempotencyKey, req.Amount)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "transfer request received",
		"data":    req,
	})
}
