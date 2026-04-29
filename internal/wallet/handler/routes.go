package handler

import "github.com/gofiber/fiber/v2"

func RegisterRoutes(router fiber.Router, h *WalletHandler) {
	wallets := router.Group("/wallets")
	wallets.Post("/", h.CreateWallet())
	wallets.Get("/", h.GetAllWallets())
	wallets.Get("/:id", h.GetWallet())
	wallets.Get("/:id/balance", h.GetBalance())
}
