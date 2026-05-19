package routes

import (
	"wallet-transfer-assignment/internal/api/handlers"

	"github.com/gofiber/fiber/v2"
)

func RegisterHealthRoute(app *fiber.App) {
	app.Get("/health", handlers.HealthHandler)
}
