package handler

import (
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"

	"github.com/labstack/echo/v4"
)

// TransferHandler exposes HTTP endpoints for transfers.
type TransferHandler struct {
	transferService service.TransferService
}

// NewTransferHandler constructs a TransferHandler.
func NewTransferHandler(transferService service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

// CreateTransfer handles POST /transfers.
func (h *TransferHandler) CreateTransfer(c echo.Context) error {
	var req service.CreateTransferRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid request payload: " + err.Error()})
	}

	resp, err := h.transferService.ExecuteTransfer(c.Request().Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrMissingIdempotencyKey),
			errors.Is(err, domain.ErrSameWalletTransfer),
			errors.Is(err, domain.ErrInvalidAmount):
			return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})

		case errors.Is(err, domain.ErrSourceWalletNotFound),
			errors.Is(err, domain.ErrDestinationWalletNotFound),
			errors.Is(err, domain.ErrWalletNotFound):
			if resp != nil {
				return c.JSON(http.StatusNotFound, resp)
			}
			return c.JSON(http.StatusNotFound, echo.Map{"error": err.Error()})

		case errors.Is(err, domain.ErrIdempotencyConflict):
			return c.JSON(http.StatusConflict, echo.Map{"error": err.Error()})

		case errors.Is(err, domain.ErrIdempotencyPayloadMismatch):
			return c.JSON(http.StatusUnprocessableEntity, echo.Map{"error": err.Error()})

		case errors.Is(err, domain.ErrInsufficientBalance):
			if resp != nil {
				return c.JSON(http.StatusUnprocessableEntity, resp)
			}
			return c.JSON(http.StatusUnprocessableEntity, echo.Map{"error": err.Error()})

		case errors.Is(err, domain.ErrCurrencyMismatch):
			if resp != nil {
				return c.JSON(http.StatusUnprocessableEntity, resp)
			}
			return c.JSON(http.StatusUnprocessableEntity, echo.Map{"error": err.Error()})

		case errors.Is(err, domain.ErrWalletInactive):
			return c.JSON(http.StatusUnprocessableEntity, echo.Map{"error": err.Error()})

		default:
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal transfer error: " + err.Error()})
		}
	}

	// If this was a replayed idempotent request, return 200 OK; otherwise 201 Created
	if resp.IsReplay {
		return c.JSON(http.StatusOK, resp)
	}
	return c.JSON(http.StatusCreated, resp)
}

// GetTransfer handles GET /transfers/:id.
func (h *TransferHandler) GetTransfer(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "transfer id is required"})
	}

	transfer, err := h.transferService.GetTransfer(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrTransferNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "transfer not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, transfer)
}

// GetTransferLedger handles GET /transfers/:id/ledger.
func (h *TransferHandler) GetTransferLedger(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "transfer id is required"})
	}

	entries, err := h.transferService.GetTransferLedger(c.Request().Context(), id)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"transferId": id,
		"entries":    entries,
	})
}
