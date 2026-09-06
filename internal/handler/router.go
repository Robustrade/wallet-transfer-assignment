package handler

import "net/http"

// NewRouter wires up all HTTP routes using the Go 1.22 stdlib ServeMux,
// which supports method matching and path parameters natively — no router
// dependency needed for a service this size.
func NewRouter(transferHandler *TransferHandler, walletHandler *WalletHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /transfers", transferHandler.CreateTransfer)
	mux.HandleFunc("POST /wallets", walletHandler.CreateWallet)
	mux.HandleFunc("GET /wallets/{id}", func(w http.ResponseWriter, r *http.Request) {
		walletHandler.GetWallet(w, r, r.PathValue("id"))
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return mux
}
