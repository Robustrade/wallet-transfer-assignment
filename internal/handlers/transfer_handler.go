package handlers

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/parth/wallet-transfer-assignment/internal/domain"
	"github.com/parth/wallet-transfer-assignment/internal/services"
)

type TransferHandler struct {
	service *services.TransferService
}

func NewTransferHandler(service *services.TransferService) *TransferHandler {
	return &TransferHandler{service: service}
}

// PostTransfer handles POST /transfers - initiates a wallet transfer
func (h *TransferHandler) PostTransfer(c *gin.Context) {
	var req domain.TransferRequest

	// Parse JSON request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body: " + err.Error()})
		return
	}

	// Call service to process transfer
	response, err := h.service.ProcessTransfer(c.Request.Context(), &req)
	if err != nil {
		// Map custom errors to HTTP status codes
		switch {
		case errors.Is(err, domain.ErrInvalidIdempotencyKey):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrInvalidWalletID):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrInvalidAmountFormat):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrInvalidAmount):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrSameWallet):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrSourceWalletNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrDestinationWalletNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		case errors.Is(err, domain.ErrInsufficientBalance):
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		default:
			// Generic server error
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		}
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetTransfer handles GET /transfers/:transfer_id - retrieves transfer status
func (h *TransferHandler) GetTransfer(c *gin.Context) {
	transferID := c.Param("transfer_id")

	transfer, err := h.service.GetTransferStatus(c.Request.Context(), transferID)
	if err != nil {
		if errors.Is(err, domain.ErrTransferNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Transfer not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Internal server error"})
		return
	}

	c.JSON(http.StatusOK, transfer)
}

// RegisterRoutes registers all transfer-related routes
func RegisterRoutes(router *gin.Engine, service *services.TransferService) {
	handler := NewTransferHandler(service)

	// Transfer endpoints
	router.POST("/transfers", handler.PostTransfer)
	router.GET("/transfers/:transfer_id", handler.GetTransfer)

	// Health check
	router.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "healthy"})
	})
}
