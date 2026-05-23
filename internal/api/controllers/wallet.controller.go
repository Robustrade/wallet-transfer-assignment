package controllers

import (
	"errors"
	"fmt"
	"shindechandrakant/internal/api/dtos"
	"shindechandrakant/internal/wallet"

	"github.com/go-playground/validator/v10"
	"github.com/gofiber/fiber/v3"
)

var validate = validator.New(validator.WithRequiredStructEnabled())

type WalletController struct {
	service wallet.Service
}

func NewWalletController(service wallet.Service) *WalletController {
	return &WalletController{service: service}
}

func (h *WalletController) Transfer(ctx fiber.Ctx) error {
	var body dtos.TransferRequest
	if err := ctx.Bind().Body(&body); err != nil {
		return ctx.Status(fiber.StatusBadRequest).JSON(dtos.ErrorResponse{
			Error: "invalid request body",
		})
	}

	if err := validate.Struct(body); err != nil {
		var ve validator.ValidationErrors
		if !errors.As(err, &ve) {
			return ctx.Status(fiber.StatusUnprocessableEntity).JSON(dtos.ErrorResponse{
				Error: "validation failed",
			})
		}
		details := make([]string, 0, len(ve))
		for _, e := range ve {
			details = append(details, fmt.Sprintf("%s failed on %s", e.Field(), e.Tag()))
		}
		return ctx.Status(fiber.StatusUnprocessableEntity).JSON(dtos.ErrorResponse{
			Error:   "validation failed",
			Details: details,
		})
	}

	transfer, err := h.service.Transfer(ctx.Context(), body)
	if err != nil {
		return mapError(ctx, err)
	}

	return ctx.Status(fiber.StatusCreated).JSON(dtos.TransferResponse{
		TransactionId:  transfer.TransactionId,
		IdempotencyKey: transfer.IdempotencyKey,
		FromWalletId:   transfer.FromWalletId,
		ToWalletId:     transfer.ToWalletId,
		Amount:         float64(transfer.Amount) / 100,
		Status:         string(transfer.State),
		CreatedAt:      transfer.CreatedAt,
	})
}

func mapError(ctx fiber.Ctx, err error) error {
	switch {
	case errors.Is(err, wallet.ErrSameWallet),
		errors.Is(err, wallet.ErrWalletInactive),
		errors.Is(err, wallet.ErrInsufficientFunds):
		return ctx.Status(fiber.StatusUnprocessableEntity).JSON(dtos.ErrorResponse{Error: err.Error()})
	case errors.Is(err, wallet.ErrWalletNotFound):
		return ctx.Status(fiber.StatusNotFound).JSON(dtos.ErrorResponse{Error: err.Error()})
	default:
		return ctx.Status(fiber.StatusInternalServerError).JSON(dtos.ErrorResponse{Error: "internal server error"})
	}
}
