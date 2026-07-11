package handler

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/platform/logger"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/service"
)

// transferService is the subset of *service.TransferService this handler
// needs, so tests can supply a mock without touching Postgres/Redis.
type transferService interface {
	InitTransfer() int64
	Transfer(ctx context.Context, req service.TransferRequest) (*domain.Transfer, error)
	GetTransfer(ctx context.Context, id int64) (*domain.Transfer, error)
}

// TransferHandler serves the /transfers/init, /transfers, and
// /transfers/:id routes. Decode -> call service -> encode, nothing else.
type TransferHandler struct {
	service transferService
	logger  *slog.Logger
}

// NewTransferHandler constructs a TransferHandler.
func NewTransferHandler(svc transferService, logger *slog.Logger) *TransferHandler {
	return &TransferHandler{service: svc, logger: logger}
}

// InitTransfer handles POST /transfers/init.
func (h *TransferHandler) InitTransfer(c *gin.Context) {
	id := h.service.InitTransfer()
	c.JSON(http.StatusOK, InitTransferResponse{TransactionID: strconv.FormatInt(id, 10)})
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(c *gin.Context) {
	var req CreateTransferRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	id, err := validateIdempotencyKey(req.IdempotencyKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateWalletID(req.FromWalletID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateWalletID(req.ToWalletID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	amount, minorUnits, err := validateAmount(req.Amount)
	if err != nil {
		var domainErr *domain.Error
		if errors.As(err, &domainErr) {
			h.respondError(c, err)
			return
		}
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	transfer, err := h.service.Transfer(c.Request.Context(), service.TransferRequest{
		ID:               id,
		FromWalletID:     req.FromWalletID,
		ToWalletID:       req.ToWalletID,
		Amount:           amount,
		AmountMinorUnits: minorUnits,
	})
	if err != nil {
		h.respondError(c, err)
		return
	}

	c.JSON(statusForTransfer(transfer), toTransferView(transfer))
}

// GetTransfer handles GET /transfers/:id.
func (h *TransferHandler) GetTransfer(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid transfer id"})
		return
	}

	transfer, err := h.service.GetTransfer(c.Request.Context(), id)
	if err != nil {
		h.respondError(c, err)
		return
	}
	if transfer == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "transfer not found"})
		return
	}

	c.JSON(http.StatusOK, toTransferView(transfer))
}

// respondError maps a *domain.Error to its typed HTTP status and body.
// Anything else is unexpected: full details logged server-side, a
// generic message returned to the client — never leak internals in a
// financial API.
func (h *TransferHandler) respondError(c *gin.Context, err error) {
	var domainErr *domain.Error
	if errors.As(err, &domainErr) {
		c.JSON(domainErrorHTTPStatus(domainErr.Code), gin.H{"error": domainErr.Code, "message": domainErr.Message})
		return
	}

	logger.FromContext(c.Request.Context(), h.logger).Error("unexpected transfer error", slog.Any("error", err))
	c.JSON(http.StatusInternalServerError, gin.H{"error": "internal error"})
}
