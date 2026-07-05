package handler

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/labstack/echo/v4"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	query *service.WalletQueryService
}

func NewWalletHandler(q *service.WalletQueryService) *WalletHandler {
	return &WalletHandler{query: q}
}

func (h *WalletHandler) GetBalance(c echo.Context) error {
	walletID := c.Param("id")
	if walletID == "" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "wallet id is required"})
	}

	result, err := h.query.GetBalance(c.Request().Context(), walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return c.JSON(http.StatusNotFound, errorResponse{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}

	return c.JSON(http.StatusOK, result)
}

func (h *WalletHandler) GetHistory(c echo.Context) error {
	walletID := c.Param("id")
	if walletID == "" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "wallet id is required"})
	}

	limit := 20
	if l := c.QueryParam("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 100 {
			limit = parsed
		}
	}
	offset := 0
	if o := c.QueryParam("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	items, err := h.query.GetHistory(c.Request().Context(), walletID, limit, offset)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			return c.JSON(http.StatusNotFound, errorResponse{Error: err.Error()})
		}
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}

	return c.JSON(http.StatusOK, items)
}
