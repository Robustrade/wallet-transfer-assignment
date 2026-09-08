package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

func NewRouter(walletHandler *WalletHandler, transferHandler *TransferHandler) http.Handler {
	r := chi.NewRouter()

	r.Post("/wallets", walletHandler.CreateWallet)
	r.Get("/wallets/{id}", walletHandler.GetWallet)

	r.Post("/transfers", transferHandler.CreateTransfer)

	return r
}
