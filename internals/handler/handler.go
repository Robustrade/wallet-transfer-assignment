package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/models"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/repository"
	"github.com/AbhishekBadgujar/wallet-transfer-assignment/internals/service"
	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
)

// transferCreator is the slice of TransferService the handler depends on.
// Depending on this narrow interface instead of *service.TransferService
// directly lets the handler be unit tested with a fake, without a database.
type transferCreator interface {
	CreateTransfer(ctx context.Context, in service.CreateTransferInput) (*service.TransferResult, error)
}

// Handler struct - responsible for no business logic
type TransferHandler struct {
	transferService transferCreator
}

func NewTransferHandler(transferService transferCreator) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

// createTransferRequest is the request body specification as per documentation
type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type errorResponse struct {
	Error string `json:"error"`
}

// CreateTransfer handles handles the main API POST /transfers.
func (h *TransferHandler) CreateTransfer(c echo.Context) error {
	var req createTransferRequest
	// Handle input validation
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request body"})
	}

	if req.IdempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "idempotencyKey is required"})
	}

	fromWalletID, err := uuid.Parse(req.FromWalletID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "fromWalletId must be a valid UUID"})
	}
	toWalletID, err := uuid.Parse(req.ToWalletID)
	if err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "toWalletId must be a valid UUID"})
	}
	if req.Amount <= 0 {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "amount must be positive"})
	}

	result, err := h.transferService.CreateTransfer(c.Request().Context(), service.CreateTransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   fromWalletID,
		ToWalletID:     toWalletID,
		Amount:         req.Amount,
	})

	// map to our custom error messages
	if err != nil {
		return mapServiceError(c, err)
	}

	status := http.StatusCreated

	// Handle failed transaction state
	if result.State == models.TransferFailed {
		status = http.StatusUnprocessableEntity
	}
	return c.JSON(status, result)
}

// mapServiceError translates known our custom service/domain errors to HTTP status
// codes. Anything unrecognized becomes a 500 Status code
func mapServiceError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, service.ErrIdempotencyKeyConflict):
		return c.JSON(http.StatusConflict, errorResponse{Error: err.Error()})
	case errors.Is(err, models.ErrSameWallet),
		errors.Is(err, models.ErrNonPositiveAmount):
		return c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
	case errors.Is(err, repository.ErrWalletNotFound):
		return c.JSON(http.StatusNotFound, errorResponse{Error: err.Error()})
	case errors.Is(err, models.ErrInsufficientBalance):
		return c.JSON(http.StatusUnprocessableEntity, errorResponse{Error: err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}
