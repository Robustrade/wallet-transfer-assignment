package handler

import (
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/observability"
	"wallet-transfer-assignment/internal/service"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type TransferHandler struct {
	transferService *service.TransferService
}

func NewTransferHandler(transferService *service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var input service.CreateTransferInput
	if err := c.ShouldBindJSON(&input); err != nil {
		respondError(c, domain.ErrInvalidInput("invalid JSON request body"))
		return
	}

	resp, cached, err := h.transferService.CreateTransfer(c.Request.Context(), &input)
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			if appErr.Code == domain.ErrCodeInsufficientFunds {
				observability.DefaultMetrics.IncInsufficientFunds()
				observability.DefaultMetrics.IncFailedTransfers()
			} else {
				observability.DefaultMetrics.IncFailedTransfers()
			}
			respondError(c, appErr)
			return
		}
		observability.DefaultMetrics.IncFailedTransfers()
		respondError(c, domain.ErrInternal(err))
		return
	}

	// Replay cached response if matching idempotency record was hit
	if cached != nil {
		c.Data(cached.StatusCode, "application/json; charset=utf-8", cached.Body)
		return
	}

	observability.DefaultMetrics.IncSuccessfulTransfers()
	c.JSON(http.StatusCreated, resp)
}

func (h *TransferHandler) GetTransfer(c *gin.Context) {
	idStr := c.Param("id")
	id, err := uuid.Parse(idStr)
	if err != nil {
		respondError(c, domain.ErrInvalidInput("transfer id must be a valid UUID"))
		return
	}

	resp, err := h.transferService.GetTransferByID(c.Request.Context(), id)
	if err != nil {
		var appErr *domain.AppError
		if errors.As(err, &appErr) {
			respondError(c, appErr)
			return
		}
		respondError(c, domain.ErrInternal(err))
		return
	}

	c.JSON(http.StatusOK, resp)
}

func respondError(c *gin.Context, appErr *domain.AppError) {
	c.JSON(appErr.HTTPStatus, gin.H{
		"error": gin.H{
			"code":    appErr.Code,
			"message": appErr.Message,
		},
	})
}
