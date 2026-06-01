package handler

import "github.com/go-chi/chi/v5"

func SetupRoutes(r chi.Router, transferH *TransferHandler, walletH *WalletHandler) {
	r.Post("/transfers", transferH.CreateTransfer)
	r.Get("/wallets/{walletID}", walletH.GetWallet)
}
