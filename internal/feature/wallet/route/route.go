package route

import (
	"wallet-transfer-assignment/internal/feature/wallet/handler"

	"github.com/gofiber/fiber/v3"
)

type Handlers struct {
	Transfer *handler.TransferHandler
}

func RegisterRoutes(app *fiber.App, handlers Handlers) {
	app.Post("/transfers", handlers.Transfer.CreateTransfer)
}
