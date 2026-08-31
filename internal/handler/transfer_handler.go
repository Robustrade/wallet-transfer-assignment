package handler

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/service"
)

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	FromWalletID   string `json:"fromWalletId" binding:"required"`
	ToWalletID     string `json:"toWalletId" binding:"required"`
	Amount         int64  `json:"amount" binding:"required,gt=0"`
}

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(
	service *service.TransferService,
) *TransferHandler {

	return &TransferHandler{
		service: service,
	}
}

func (h *TransferHandler) CreateTransfer(
	c *gin.Context,
) {

	var req CreateTransferRequest

	// --------------------------------------------------------
	// Parse request
	// --------------------------------------------------------

	if err := c.ShouldBindJSON(&req); err != nil {

		c.JSON(
			http.StatusBadRequest,
			gin.H{
				"error":   "invalid request",
				"details": err.Error(),
			},
		)

		return
	}

	// --------------------------------------------------------
	// Call service
	// --------------------------------------------------------

	input := service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	}

	response, err := h.service.CreateTransfer(
		c.Request.Context(),
		input,
	)

	if err != nil {

		if errors.Is(
			err,
			service.ErrInsufficientBalance,
		) {
			c.JSON(
				http.StatusUnprocessableEntity,
				gin.H{
					"error": err.Error(),
				},
			)
			return
		}

		if errors.Is(
			err,
			service.ErrWalletNotFound,
		) {
			c.JSON(
				http.StatusNotFound,
				gin.H{
					"error": err.Error(),
				},
			)
			return
		}

		if errors.Is(
			err,
			service.ErrIdempotencyConflict,
		) {
			c.JSON(
				http.StatusConflict,
				gin.H{
					"error": err.Error(),
				},
			)
			return
		}

		c.JSON(
			http.StatusInternalServerError,
			gin.H{
				"error": "failed to create transfer",
			},
		)

		return
	}

	// --------------------------------------------------------
	// Success
	// --------------------------------------------------------

	c.JSON(
		http.StatusOK,
		response,
	)
}
