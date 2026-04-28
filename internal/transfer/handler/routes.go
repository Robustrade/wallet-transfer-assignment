package handler

import "github.com/gofiber/fiber/v2"

// RegisterRoutes mounts transfer routes onto the given router group.
func RegisterRoutes(router fiber.Router, h *TransferHandler) {
	transfers := router.Group("/transfers")
	transfers.Post("/", h.CreateTransfer())
	transfers.Get("/", h.GetAllTransfers())
	transfers.Get("/:id", h.GetTransfer())
}
