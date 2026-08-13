package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	service *service.WalletService
}

func NewWalletHandler(svc *service.WalletService) *WalletHandler {
	return &WalletHandler{service: svc}
}

func (h *WalletHandler) Create(c *gin.Context) {
	var req CreateWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: err.Error()})
		return
	}

	result, err := h.service.CreateWallet(c.Request.Context(), service.CreateWalletInput{
		ID:       req.ID,
		Balance:  req.Balance,
		Currency: req.Currency,
	})
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusCreated, toWalletResponse(result))
}

func (h *WalletHandler) Get(c *gin.Context) {
	result, err := h.service.GetWallet(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, toWalletResponse(result))
}

func (h *WalletHandler) Ledger(c *gin.Context) {
	entries, err := h.service.ListLedger(c.Request.Context(), c.Param("id"))
	if err != nil {
		writeError(c, err)
		return
	}

	resp := make([]LedgerEntryResponse, 0, len(entries))
	for _, e := range entries {
		resp = append(resp, toLedgerEntryResponse(e))
	}
	c.JSON(http.StatusOK, resp)
}
