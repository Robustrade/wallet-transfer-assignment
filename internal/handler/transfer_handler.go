package handler

import (
	"errors"
	"net/http"

	"github.com/labstack/echo/v4"

	"github.com/karthikadarvin/wallet-transfer-assignment/internal/domain"
	"github.com/karthikadarvin/wallet-transfer-assignment/internal/service"
)

type TransferHandler struct {
	service *service.TransferService
}

func NewTransferHandler(s *service.TransferService) *TransferHandler {
	return &TransferHandler{service: s}
}

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type errorResponse struct {
	Error string `json:"error"`
}

func (h *TransferHandler) Create(c echo.Context) error {
	var req createTransferRequest
	if err := c.Bind(&req); err != nil {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "invalid request body"})
	}

	if req.IdempotencyKey == "" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: domain.ErrIdempotencyKeyRequired.Error()})
	}
	if req.FromWalletID == "" || req.ToWalletID == "" {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: "fromWalletId and toWalletId are required"})
	}
	if req.Amount <= 0 {
		return c.JSON(http.StatusBadRequest, errorResponse{Error: domain.ErrInvalidAmount.Error()})
	}

	result, err := h.service.ExecuteTransfer(c.Request().Context(), service.TransferInput{
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
	})
	if err != nil {
		return mapTransferError(c, err)
	}

	return c.JSON(http.StatusCreated, result)
}

func mapTransferError(c echo.Context, err error) error {
	switch {
	case errors.Is(err, domain.ErrWalletNotFound):
		return c.JSON(http.StatusNotFound, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrSameWallet),
		errors.Is(err, domain.ErrInvalidAmount),
		errors.Is(err, domain.ErrIdempotencyKeyRequired):
		return c.JSON(http.StatusBadRequest, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrIdempotencyKeyConflict):
		return c.JSON(http.StatusConflict, errorResponse{Error: err.Error()})
	case errors.Is(err, domain.ErrIdempotencyInProgress):
		return c.JSON(http.StatusConflict, errorResponse{Error: err.Error()})
	default:
		return c.JSON(http.StatusInternalServerError, errorResponse{Error: "internal server error"})
	}
}
