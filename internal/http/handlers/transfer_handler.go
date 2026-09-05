package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type TransferService interface {
	Transfer(ctx context.Context, req domain.TransferRequest) (service.TransferResult, error)
}

type TransferHandler struct {
	transfers TransferService
}

func NewTransferHandler(transfers TransferService) *TransferHandler {
	return &TransferHandler{
		transfers: transfers,
	}
}

func (h *TransferHandler) Create(c *gin.Context) {
	var body createTransferRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		RespondError(c, bindingError(err))
		return
	}

	start := time.Now()
	result, err := h.transfers.Transfer(c.Request.Context(), body.toDomain())
	duration := time.Since(start)

	if err != nil {
		slog.Warn("transfer rejected",
			"fromWallet", body.FromWalletID,
			"toWallet", body.ToWalletID,
			"error", err,
			"durationMs", duration.Milliseconds(),
		)
		RespondError(c, err)
		return
	}

	slog.Info("transfer processed",
		"transferId", result.TransferID.String(),
		"fromWallet", result.FromWalletID,
		"toWallet", result.ToWalletID,
		"amount", int64(result.Amount),
		"currency", result.Currency,
		"durationMs", duration.Milliseconds(),
	)

	c.JSON(http.StatusCreated, transferResponseFromResult(result))
}

func bindingError(err error) error {
	return &domain.ValidationError{
		Field:   "body",
		Message: "request body is malformed or missing required fields: " + err.Error(),
	}
}
