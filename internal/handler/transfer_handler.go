package handler

import (
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"wallet-transfer-service/internal/model"
	"wallet-transfer-service/internal/service"
)

type TransferHandler struct {
	transferService service.TransferService
}

func NewTransferHandler(transferService service.TransferService) *TransferHandler {
	return &TransferHandler{
		transferService: transferService,
	}
}

// CreateTransfer handles POST /api/v1/transfers
func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var req model.CreateTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		slog.WarnContext(c.Request.Context(), "create transfer request validation failed", "error", err)
		HandleValidationError(c, err)
		return
	}
	slog.DebugContext(c.Request.Context(), "create transfer request received", "from_wallet_id", req.FromWalletID, "to_wallet_id", req.ToWalletID)

	res, statusCode, err := h.transferService.Transfer(c.Request.Context(), &req)
	if err != nil {
		if statusCode == 0 {
			statusCode = http.StatusInternalServerError
		}

		// Do not send internal database / driver errors to the client
		if statusCode >= http.StatusInternalServerError {
			c.JSON(statusCode, gin.H{"error": "transfer processing failed"})
			slog.ErrorContext(c.Request.Context(), "create transfer request failed", "from_wallet_id", req.FromWalletID, "to_wallet_id", req.ToWalletID, "status", statusCode, "error", err)
			return
		}

		// Renders domain errors and replayed idempotent failures (400, 403, 404, 409) as warnings
		c.JSON(statusCode, gin.H{"error": err.Error()})
		slog.WarnContext(c.Request.Context(), "create transfer request rejected", "from_wallet_id", req.FromWalletID, "to_wallet_id", req.ToWalletID, "status", statusCode, "reason", err.Error())
		return
	}

	if statusCode == 0 {
		statusCode = http.StatusCreated
	}

	c.JSON(statusCode, res)
	slog.InfoContext(c.Request.Context(), "create transfer request completed", "transfer_id", res.TransferID, "status", statusCode)
}
