package handlers

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

const ReplayHeader = "Idempotent-Replay"

type TransferService interface {
	Transfer(ctx context.Context, req domain.TransferRequest) (service.TransferOutcome, error)
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
	outcome, err := h.transfers.Transfer(c.Request.Context(), body.toDomain())
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

	status := http.StatusCreated
	if outcome.Replayed {
		status = http.StatusOK
	}

	slog.Info("transfer processed",
		"transferId", outcome.Result.TransferID.String(),
		"fromWallet", outcome.Result.FromWalletID,
		"toWallet", outcome.Result.ToWalletID,
		"amount", int64(outcome.Result.Amount),
		"currency", outcome.Result.Currency,
		"replayed", outcome.Replayed,
		"durationMs", duration.Milliseconds(),
	)

	c.Header(ReplayHeader, strconv.FormatBool(outcome.Replayed))
	c.JSON(status, transferResponseFromResult(outcome.Result))
}

func bindingError(err error) error {
	return &domain.ValidationError{
		Field:   "body",
		Message: "request body is malformed or missing required fields: " + err.Error(),
	}
}
