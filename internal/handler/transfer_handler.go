package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/handler/dto"
	"wallet-transfer-assignment/internal/service"
)

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
	c echo.Context,
) error {
	var req dto.CreateTransferRequest

	if err := c.Bind(&req); err != nil {
		return c.JSON(
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid request body",
			},
		)
	}

	if err := c.Validate(&req); err != nil {
		return c.JSON(
			http.StatusBadRequest,
			map[string]string{
				"error": "invalid request",
			},
		)
	}

	result, err := h.service.CreateTransfer(
		c.Request().Context(),
		req.ToDomain(),
	)

	if err != nil {
		return h.handleError(c, result, err)
	}

	status := http.StatusCreated

	if result.Replayed {
		status = http.StatusOK
	}

	return c.JSON(
		status,
		dto.FromTransfer(result.Transfer),
	)
}

func (h *TransferHandler) handleError(
	c echo.Context,
	result *service.CreateTransferResult,
	err error,
) error {
	switch {

	case errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrIdempotencyKeyRequired):

		return c.JSON(
			http.StatusBadRequest,
			map[string]string{
				"error": err.Error(),
			},
		)

	case errors.Is(err, domain.ErrIdempotencyConflict):
		return c.JSON(
			http.StatusConflict,
			map[string]string{
				"error": err.Error(),
			},
		)

	case errors.Is(err, domain.ErrWalletNotFound):
		return c.JSON(
			http.StatusNotFound,
			failedTransferResponse(result, err),
		)

	case errors.Is(err, domain.ErrInsufficientFunds):
		return c.JSON(
			http.StatusUnprocessableEntity,
			failedTransferResponse(result, err),
		)

	default:
		return c.JSON(
			http.StatusInternalServerError,
			map[string]string{
				"error": "internal server error",
			},
		)
	}
}

func failedTransferResponse(
	result *service.CreateTransferResult,
	err error,
) map[string]any {
	response := map[string]any{
		"error": err.Error(),
	}

	if result != nil && result.Transfer != nil {
		response["transfer"] = dto.FromTransfer(
			result.Transfer,
		)
	}

	return response
}
