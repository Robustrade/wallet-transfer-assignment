package handler

import (
	"net/http"

	"wallet-transfer-service/internal/service"

	"github.com/gin-gonic/gin"
)

type WalletHandler struct {
	walletService *service.WalletService
}

func NewWalletHandler(
	walletService *service.WalletService,
) *WalletHandler {

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
