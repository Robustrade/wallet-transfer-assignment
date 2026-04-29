package handler

import (
	"log"
	"net/http"
	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"

	"github.com/gin-gonic/gin"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(service *service.TransferService) *TransferHandler {
	return &TransferHandler{service: service}
}

// ErrorResponse represents an error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message,omitempty"`
}

// CreateTransfer handles POST /transfers
func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var req service.TransferRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		log.Printf("Invalid request: %v", err)
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_REQUEST",
			Message: err.Error(),
		})
		return
	}

	// Additional validation
	if req.Amount <= 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_AMOUNT",
			Message: "Amount must be positive",
		})
		return
	}

	if req.FromWalletID == req.ToWalletID {
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "SAME_WALLET",
			Message: "Cannot transfer to the same wallet",
		})
		return
	}

	// Execute transfer
	response, err := h.service.CreateTransfer(c.Request.Context(), req)
	if err != nil {
		h.handleError(c, err)
		return
	}

	c.JSON(http.StatusOK, response)
}

// GetTransfer handles GET /transfers/:id
func (h *TransferHandler) GetTransfer(c *gin.Context) {
	transferID := c.Param("id")

	response, err := h.service.GetTransfer(c.Request.Context(), transferID)
	if err != nil {
		if err == domain.ErrTransferNotFound {
			c.JSON(http.StatusNotFound, ErrorResponse{
				Error:   "NOT_FOUND",
				Message: "Transfer not found",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "Failed to retrieve transfer",
		})
		return
	}

	c.JSON(http.StatusOK, response)
}

// handleError maps domain errors to HTTP responses
func (h *TransferHandler) handleError(c *gin.Context, err error) {
	log.Printf("Error processing transfer: %v", err)

	switch err {
	case domain.ErrInsufficientFunds:
		c.JSON(http.StatusUnprocessableEntity, ErrorResponse{
			Error:   "INSUFFICIENT_FUNDS",
			Message: "Insufficient funds in source wallet",
		})
	case domain.ErrWalletNotFound:
		c.JSON(http.StatusNotFound, ErrorResponse{
			Error:   "WALLET_NOT_FOUND",
			Message: "One or more wallets not found",
		})
	case domain.ErrInvalidAmount:
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "INVALID_AMOUNT",
			Message: "Amount must be positive",
		})
	case domain.ErrSameWallet:
		c.JSON(http.StatusBadRequest, ErrorResponse{
			Error:   "SAME_WALLET",
			Message: "Cannot transfer to the same wallet",
		})
	default:
		c.JSON(http.StatusInternalServerError, ErrorResponse{
			Error:   "INTERNAL_ERROR",
			Message: "An unexpected error occurred",
		})
	}
}
