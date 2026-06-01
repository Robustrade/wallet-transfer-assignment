package ledger

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes ledger/balance operations over HTTP.
// Intentionally thin — request parsing and response mapping only; no business logic.
type Handler struct {
	svc *Service
}

// NewHandler constructs a ledger HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts ledger routes onto the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/wallet/balance/:walletId", h.GetBalance)
}

// GetBalance handles GET /wallet/balance/:walletId
func (h *Handler) GetBalance(c *gin.Context) {
	walletID := c.Param("walletId")
	if walletID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"data": nil, "error": "walletId is required", "status": http.StatusBadRequest,
		})
		return
	}

	b, err := h.svc.GetBalance(c.Request.Context(), walletID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusNotFound,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data": gin.H{
			"wallet_id":  b.WalletID,
			"balance":    b.Amount,
			"updated_at": b.UpdatedAt,
		},
		"error":  nil,
		"status": http.StatusOK,
	})
}
