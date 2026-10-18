package handler

import (
	"errors"
	"fmt"
	"strings"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/repository"
	"wallet-transfer-assignment/internal/feature/wallet/service"

	"github.com/gofiber/fiber/v3"
	"gorm.io/gorm"
)

type TransferHandler struct {
	transferService service.TransferService
}

func NewTransferHandler(transferService service.TransferService) *TransferHandler {
	return &TransferHandler{transferService: transferService}
}

type createTransferBody struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   int64  `json:"fromWalletId"`
	ToWalletID     int64  `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// POST /transfers

func (h *TransferHandler) CreateTransfer(c fiber.Ctx) error {
	var req createTransferBody
	// 1. Parse request
	if err := c.Bind().JSON(&req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "invalid request body",
		})
	}
	// 2. Validate request
	if err := validateTransferRequest(req); err != nil {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": err.Error(),
		})
	}

	result, err := h.transferService.CreateTransfer(
		c.Context(),
		service.CreateTransferRequest{
			IdempotencyKey: req.IdempotencyKey,
			FromWalletID:   req.FromWalletID,
			ToWalletID:     req.ToWalletID,
			Amount:         req.Amount,
		},
	)
	if err != nil {
		return handleTransferError(c, err)
	}
	// 4. Return service response.
	return c.Status(result.StatusCode).Send(result.Body)
}

func validateTransferRequest(req createTransferBody) error {
	if strings.TrimSpace(req.IdempotencyKey) == "" ||
		len(req.IdempotencyKey) < domain.MinIdempotencyKeyLength ||
		len(req.IdempotencyKey) > domain.MaxIdempotencyKeyLength {
		return fmt.Errorf("idempotencyKey must contain %d to %d characters",
			domain.MinIdempotencyKeyLength, domain.MaxIdempotencyKeyLength)
	}
	if req.FromWalletID <= 0 {
		return errors.New("fromWalletId must be greater than zero")
	}
	if req.ToWalletID <= 0 {
		return errors.New("toWalletId must be greater than zero")
	}
	if req.FromWalletID == req.ToWalletID {
		return errors.New("sender and receiver wallets must be different")
	}
	if req.Amount <= 0 {
		return errors.New("amount must be greater than zero")
	}
	return nil
}

func handleTransferError(c fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidTransferRequest):
		return c.Status(400).JSON(fiber.Map{
			"error": "invalid transfer request",
		})

	case errors.Is(err, domain.ErrIdempotencyConflict):
		return c.Status(409).JSON(fiber.Map{
			"error": "idempotency key already used with different request",
		})

	case errors.Is(err, service.ErrRequestInProgress):
		return c.Status(409).JSON(fiber.Map{
			"error": "request with this idempotency key is still in progress; retry shortly",
		})

	case errors.Is(err, domain.ErrWalletNotFound),
		errors.Is(err, gorm.ErrRecordNotFound):
		return c.Status(404).JSON(fiber.Map{
			"error": "wallet not found",
		})

	case errors.Is(err, repository.ErrCommitUncertain):
		return c.Status(503).JSON(fiber.Map{
			"error": "transfer outcome uncertain; retry using the same idempotency key",
		})

	default:
		return c.Status(500).JSON(fiber.Map{
			"error": "internal server error",
		})
	}
}
