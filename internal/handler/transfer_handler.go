package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/trjade/wallet-transfer-service/internal/domain"
	"github.com/trjade/wallet-transfer-service/internal/dto"
	"github.com/trjade/wallet-transfer-service/internal/service"
)

type TransferHandler struct {
	transferService *service.TransferService
}

func NewTransferHandler(transferService *service.TransferService) *TransferHandler {
	return &TransferHandler{
		transferService: transferService,
	}
}

func (h *TransferHandler) validate(req dto.CreateTransferRequest) error {
	if req.IdempotencyKey == "" {
		return domain.ErrInvalidIdempotencyKey
	}
	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWalletTransfer
	}

	if req.FromWalletID == uuid.Nil {
		return errors.New("from_wallet_id is required")
	}

	if req.ToWalletID == uuid.Nil {
		return errors.New("to_wallet_id is required")
	}

	if req.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}

	return nil
}

func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	// Validate the request
	var request dto.CreateTransferRequest
	if err := c.ShouldBindJSON(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request payload"})
		return
	}

	if err := h.validate(request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Perform the transfer using the transfer service
	transfer, err := h.transferService.CreateTransfer(c.Request.Context(), request)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var response dto.TransferResponse
	response.TransferID = transfer.ID.String()
	response.Status = string(transfer.Status)

	c.JSON(http.StatusCreated, response)
	return
}
