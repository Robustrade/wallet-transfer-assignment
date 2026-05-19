package routes

import (
	"wallet-transfer-assignment/internal/api/handlers"
	"wallet-transfer-assignment/internal/wallet"

	"github.com/gofiber/fiber/v2"
)

func RegisterWalletRoutes(app *fiber.App, service wallet.WalletService) {
	app.Post("/transfers", handlers.TransferHandler(service))
	app.Post("/wallets", handlers.CreateWalletHandler(service))
	app.Get("/wallets/:id", handlers.GetWalletHandler(service))
}
