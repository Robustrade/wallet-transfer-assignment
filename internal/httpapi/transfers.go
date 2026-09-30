package httpapi

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/robustrade/wallet-transfer/internal/domain"
	"github.com/robustrade/wallet-transfer/internal/service"
)

type Handler struct{ transfers *service.Transfers }

func NewHandler(transfers *service.Transfers) *Handler { return &Handler{transfers: transfers} }

const (
	// Error codes returned in JSON responses
	ErrCodeInvalidRequest      = "invalid_request"
	ErrCodeIdempotencyConflict = "idempotency_conflict"
	ErrCodeWalletNotFound      = "wallet_not_found"
	ErrCodeInternalError       = "internal_error"
)

func (h *Handler) Register(e *echo.Echo) {
	e.POST("/transfers", h.createTransfer)
	e.GET("/wallets/:id", h.getWallet)
}

type errorResponse struct {
	Error errorBody `json:"error"`
}
type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeError(c echo.Context, status int, code, message string) error {
	return c.JSONPretty(status, errorResponse{Error: errorBody{Code: code, Message: message}}, "  ")
}

func (h *Handler) createTransfer(c echo.Context) error {
	var request domain.TransferRequest
	if err := c.Bind(&request); err != nil {
		return writeError(c, http.StatusBadRequest, "invalid_request", "request body must be valid JSON")
	}
	result, err := h.transfers.Create(c.Request().Context(), request)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidRequest):
			return writeError(c, http.StatusBadRequest, ErrCodeInvalidRequest, "provide a key, distinct wallet IDs, and a positive integer amount")
		case errors.Is(err, domain.ErrIdempotencyConflict):
			return writeError(c, http.StatusConflict, ErrCodeIdempotencyConflict, "idempotency key was already used for a different request")
		case errors.Is(err, domain.ErrWalletNotFound):
			return writeError(c, http.StatusNotFound, ErrCodeWalletNotFound, "one or both of the specified wallets do not exist")
		default:
			c.Logger().Errorf("create transfer failed: %v", err)
			return writeError(c, http.StatusInternalServerError, ErrCodeInternalError, "transfer could not be completed")
		}
	}
	return c.JSONPretty(result.HTTPStatus, result, "  ")
}

func (h *Handler) getWallet(c echo.Context) error {
	wallet, err := h.transfers.Wallet(c.Request().Context(), c.Param("id"))
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return writeError(c, http.StatusNotFound, ErrCodeWalletNotFound, "wallet does not exist")
		}
		if errors.Is(err, domain.ErrInvalidRequest) {
			return writeError(c, http.StatusBadRequest, ErrCodeInvalidRequest, "wallet ID is required")
		}
		c.Logger().Errorf("get wallet failed: %v", err)
		return writeError(c, http.StatusInternalServerError, ErrCodeInternalError, "wallet could not be loaded")
	}
	return c.JSONPretty(http.StatusOK, wallet, "  ")
}
