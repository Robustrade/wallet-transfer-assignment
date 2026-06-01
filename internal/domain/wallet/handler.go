package wallet

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// Handler exposes wallet domain operations over HTTP.
// Intentionally thin — request parsing and response mapping only; no business logic.
type Handler struct {
	svc *Service
}

// NewHandler constructs a wallet HTTP handler.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// RegisterRoutes mounts wallet routes onto the provided router group.
func (h *Handler) RegisterRoutes(rg *gin.RouterGroup) {
	rg.GET("/wallet/:id", h.GetWallet)
	rg.POST("/wallets", h.CreateWallet)
}

// GetWallet handles GET /wallet/:id
func (h *Handler) GetWallet(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"data": nil, "error": "wallet id is required", "status": http.StatusBadRequest,
		})
		return
	}

	w, err := h.svc.GetByID(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusNotFound,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": w, "error": nil, "status": http.StatusOK})
}

type createWalletRequest struct {
	OwnerID  string `json:"ownerId"  binding:"required"`
	Currency string `json:"currency" binding:"required"`
}

// CreateWallet handles POST /wallets
func (h *Handler) CreateWallet(c *gin.Context) {
	var req createWalletRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusBadRequest,
		})
		return
	}

	w, err := h.svc.Create(c.Request.Context(), req.OwnerID, req.Currency)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"data": nil, "error": err.Error(), "status": http.StatusInternalServerError,
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{"data": w, "error": nil, "status": http.StatusCreated})
}
