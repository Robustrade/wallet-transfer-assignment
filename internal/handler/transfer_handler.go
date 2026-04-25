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

	var transfer model.Transfer
	if err := c.ShouldBindJSON(&transfer); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	err := h.service.Transfer(c.Request.Context(), &transfer)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusCreated, gin.H{"transfer_id": transfer.Id})
}
