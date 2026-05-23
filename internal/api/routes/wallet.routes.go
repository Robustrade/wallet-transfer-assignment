package routes

import (
	"shindechandrakant/internal/api/controllers"

	"github.com/gofiber/fiber/v3"
)

func WalletRoutes(app fiber.Router, h *controllers.WalletController) {
	app.Post("/transfers", h.Transfer)
}
