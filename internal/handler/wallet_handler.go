package handler

import (
	"errors"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"

	"github.com/labstack/echo/v4"
)

// WalletHandler exposes HTTP endpoints for wallet queries and initialization.
type WalletHandler struct {
	walletService service.WalletService
}

// NewWalletHandler constructs a WalletHandler.
func NewWalletHandler(walletService service.WalletService) *WalletHandler {
	return &WalletHandler{walletService: walletService}
}

// CreateWallet handles POST /wallets.
func (h *WalletHandler) CreateWallet(c echo.Context) error {
	var req service.CreateWalletRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "invalid payload: " + err.Error()})
	}

	wallet, err := h.walletService.CreateWallet(c.Request().Context(), req)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusCreated, wallet)
}

// GetWallet handles GET /wallets/:id.
func (h *WalletHandler) GetWallet(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "wallet id is required"})
	}

	wallet, err := h.walletService.GetWallet(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "wallet not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, wallet)
}

// GetBalance handles GET /wallets/:id/balance.
func (h *WalletHandler) GetBalance(c echo.Context) error {
	id := c.Param("id")
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "wallet id is required"})
	}

	balResp, err := h.walletService.GetBalance(c.Request().Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "wallet not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": err.Error()})
	}

	return c.JSON(http.StatusOK, balResp)
}
