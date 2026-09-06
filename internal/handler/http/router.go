package http

import "net/http"

func NewRouter(transfers *TransferHandler, wallets *WalletHandler) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("POST /transfers", transfers.Create)
	mux.HandleFunc("GET /transfers/{id}", transfers.Get)

	mux.HandleFunc("POST /wallets", wallets.Create)
	mux.HandleFunc("GET /wallets/{id}", wallets.Get)

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	return mux
}
