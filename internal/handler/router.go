package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(walletHandler *WalletHandler) http.Handler {
	r := chi.NewRouter()

	r.Post("/wallets", walletHandler.CreateWallet)
	r.Get("/wallets/{id}", walletHandler.GetWallet)

	return r
}
