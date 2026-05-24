package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"wallet-transfer-assignment/internal/service"
)

func NewRouter(walletService *service.WalletService) http.Handler {
	r := chi.NewRouter()

	r.Get("/health", health)
	r.Post("/wallets", createWallet(walletService))
	r.Get("/wallets/{walletId}", getWallet(walletService))

	return r
}
