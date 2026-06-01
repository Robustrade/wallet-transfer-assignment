package transfer

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// Handler exposes transfer domain operations over HTTP.
// Intentionally thin — request parsing and response mapping only; no business logic.
type Handler struct {
	svc *Service
}

// NewHandler constructs a transfer HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts transfer routes onto the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.POST("/transfers", h.CreateTransfer)
	rg.POST("/transfers/fund", h.FundWallet)
}

type createTransferRequest struct {
	TxnID        string `json:"txnId"        binding:"required"`
	FromWalletID string `json:"fromWalletId" binding:"required"`
	ToWalletID   string `json:"toWalletId"   binding:"required"`
	Amount       int64  `json:"amount"       binding:"required,gt=0"`
}

// CreateTransfer handles POST /transfers
func (h *Handler) CreateTransfer(c *gin.Context) {
	var req createTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusBadRequest,
		})
		return
	}

	t, err := h.svc.Execute(c.Request.Context(), &CreateRequest{
		TxnID:        req.TxnID,
		FromWalletID: req.FromWalletID,
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusBadRequest,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"txnID":  t.TxnID,
			"status": t.Status,
		},
		"error":  nil,
		"status": http.StatusOK,
	})
}

type fundWalletRequest struct {
	ToWalletID string `json:"toWalletId" binding:"required"`
	Amount     int64  `json:"amount"     binding:"required,gt=0"`
}

// FundWallet handles POST /transfers/fund
func (h *Handler) FundWallet(c *gin.Context) {
	var req fundWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusBadRequest,
		})
		return
	}

	t, err := h.svc.Execute(c.Request.Context(), &CreateRequest{
		TxnID:        "fund_" + uuid.NewString(), // Auto-generate idempotency key for internal funding
		FromWalletID: "default_wallet",           // Seeded faucet wallet
		ToWalletID:   req.ToWalletID,
		Amount:       req.Amount,
	})
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusBadRequest,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"txnID":  t.TxnID,
			"status": t.Status,
		},
		"error":  nil,
		"status": http.StatusOK,
	})
}
