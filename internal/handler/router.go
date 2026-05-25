package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"wallet-transfer-assignment/internal/service"
)

func NewRouter(walletService *service.WalletService, transferService *service.TransferService) http.Handler {
	r := chi.NewRouter()

	r.Get("/health", health)
	r.Post("/wallets", createWallet(walletService))
	r.Get("/wallets/{walletId}", getWallet(walletService))
	r.Get("/wallets/{walletId}/ledger", getWalletLedger(transferService))
	r.Post("/transfers", createTransfer(transferService))
	r.Get("/transfers/{transferId}", getTransfer(transferService))

	return r
}
