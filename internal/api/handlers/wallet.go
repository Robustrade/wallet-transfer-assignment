package handlers

import (
	"wallet-transfer-assignment/internal/api/dtos"
	"wallet-transfer-assignment/internal/wallet"

	"github.com/gofiber/fiber/v2"
)

func TransferHandler(service wallet.WalletService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req dtos.TransferRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
		}

		tx, err := service.TransferFunds(c.Context(), req)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}

		return c.Status(200).JSON(fiber.Map{"status": tx.State, "transactionId": tx.ID})
	}
}

func CreateWalletHandler(service wallet.WalletService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		var req dtos.CreateWalletRequest
		if err := c.BodyParser(&req); err != nil {
			return c.Status(400).JSON(fiber.Map{"error": "invalid request"})
		}
		w, err := service.CreateWallet(c.Context(), req)
		if err != nil {
			return c.Status(400).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(201).JSON(fiber.Map{"walletId": w.WalletID, "balance": w.Balance})
	}
}

func GetWalletHandler(service wallet.WalletService) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := c.Params("id")
		w, err := service.GetWallet(c.Context(), id)
		if err != nil {
			return c.Status(404).JSON(fiber.Map{"error": err.Error()})
		}
		return c.Status(200).JSON(fiber.Map{"walletId": w.WalletID, "balance": w.Balance})
	}
}
