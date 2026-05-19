package handlers

import (
	"github.com/gofiber/fiber/v2"
)

func HealthHandler(c *fiber.Ctx) error {
	return c.Status(200).JSON(fiber.Map{"status": "ok"})
}
