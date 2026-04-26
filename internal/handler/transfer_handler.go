package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/robustrade/wallet-transfer-assignment/internal/model"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(s *service.TransferService) *TransferHandler {
	return &TransferHandler{service: s}
}

func (h *TransferHandler) Transfer(c *gin.Context) {

	var transferRequest model.TransferRequest
	if err := c.ShouldBindJSON(&transferRequest); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	transfer := &model.Transfer{
		IdempotencyKey: transferRequest.IdempotencyKey,
		FromWalletId:   transferRequest.FromWalletId,
		ToWalletId:     transferRequest.ToWalletId,
		Amount:         transferRequest.Amount,
	}
	err := h.service.Transfer(c.Request.Context(), transfer)
	if err != nil {
		status, msg := mapErrorToStatus(err)
		c.JSON(status, gin.H{"error": msg})
		return
	}

	c.JSON(http.StatusOK, gin.H{"transfer_id": transfer.Id})
}
