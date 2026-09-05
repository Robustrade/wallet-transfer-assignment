package handlers

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/http/middlewares"
	"wallet-transfer/internal/metrics"
	"wallet-transfer/internal/service"
)

const ReplayHeader = "Idempotent-Replay"

type TransferService interface {
	Transfer(ctx context.Context, req domain.TransferRequest) (service.TransferOutcome, error)
	Get(ctx context.Context, id uuid.UUID) (service.TransferDetail, error)
}

type TransferHandler struct {
	transfers TransferService
	metrics   *metrics.Metrics
}

func NewTransferHandler(transfers TransferService, m *metrics.Metrics) *TransferHandler {
	return &TransferHandler{
		transfers: transfers,
		metrics:   m,
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
		h.metrics.ObserveTransfer(outcomeFor(err), duration)
		slog.Warn("transfer rejected",
			"requestId", middlewares.RequestID(c),
			"fromWallet", body.FromWalletID,
			"toWallet", body.ToWalletID,
			"error", err,
			"durationMs", duration.Milliseconds(),
		)
		RespondError(c, err)
		return
	}

	status := http.StatusCreated
	metricOutcome := metrics.OutcomeCreated
	if outcome.Replayed {
		status = http.StatusOK
		metricOutcome = metrics.OutcomeReplayed
	}
	h.metrics.ObserveTransfer(metricOutcome, duration)

	slog.Info("transfer processed",
		"requestId", middlewares.RequestID(c),
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

func (h *TransferHandler) Get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		RespondError(c, &domain.ValidationError{Field: "id", Message: "must be a UUID"})
		return
	}

	detail, err := h.transfers.Get(c.Request.Context(), id)
	if err != nil {
		RespondError(c, err)
		return
	}

	c.JSON(http.StatusOK, newTransferDetailResponse(detail))
}

func bindingError(err error) error {
	return &domain.ValidationError{
		Field:   "body",
		Message: "request body is malformed or missing required fields: " + err.Error(),
	}
}

func outcomeFor(err error) string {
	switch {
	case errors.Is(err, domain.ErrValidation),
		errors.Is(err, domain.ErrInsufficientFunds),
		errors.Is(err, domain.ErrCurrencyMismatch),
		errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, domain.ErrIdempotencyKeyConflict):
		return metrics.OutcomeRejected
	default:
		return metrics.OutcomeFailed
	}
}
