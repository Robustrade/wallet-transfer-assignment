package handler

import (
	"net/http"

	"Wallet_Transfer_Service/internal/service"
)

func NewRouter(transfers *service.TransferService, wallets *service.WalletService) http.Handler {
	th := NewTransferHandler(transfers)
	wh := NewWalletHandler(wallets)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", th.CreateTransfer)
	mux.HandleFunc("GET /transfers/{id}", th.GetTransfer)
	mux.HandleFunc("POST /wallets", wh.CreateWallet)
	mux.HandleFunc("GET /wallets/{id}", wh.GetWallet)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	return mux
}
