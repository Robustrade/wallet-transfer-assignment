package handlers

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"wallet-transfer/internal/domain"
)

type WalletService interface {
	Get(ctx context.Context, id string) (domain.Wallet, error)
	History(ctx context.Context, id string, limit int) ([]domain.Transfer, error)
}

type WalletHandler struct {
	wallets WalletService
}

func NewWalletHandler(wallets WalletService) *WalletHandler {
	return &WalletHandler{wallets: wallets}
}

func (h *WalletHandler) Get(c *gin.Context) {
	wallet, err := h.wallets.Get(c.Request.Context(), c.Param("id"))
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, newWalletResponse(wallet))
}

func (h *WalletHandler) GetHistory(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	transfers, err := h.wallets.History(c.Request.Context(), c.Param("id"), limit)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, newHistoryResponse(domain.NormalizeWalletID(c.Param("id")), transfers))
}
