package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// TransferHandler serves the /transfers/init, /transfers, and
// /transfers/:id routes. Stubbed for Phase 1; the transfer flow itself
// lands in Phase 2 alongside internal/service.
type TransferHandler struct{}

// NewTransferHandler constructs a TransferHandler.
func NewTransferHandler() *TransferHandler {
	return &TransferHandler{}
}

// InitTransfer handles POST /transfers/init.
func (h *TransferHandler) InitTransfer(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}

// GetTransfer handles GET /transfers/:id.
func (h *TransferHandler) GetTransfer(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "not implemented"})
}
