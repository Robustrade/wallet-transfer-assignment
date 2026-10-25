package http

import (
	"net/http"
)

func RegisterRoutes(handler *WalletHandler) *http.ServeMux {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// V1 Wallets and Transfers Endpoints
	mux.HandleFunc("POST /api/v1/wallets", handler.CreateWallet)
	mux.HandleFunc("GET /api/v1/wallets/{id}", handler.GetWallet)
	mux.HandleFunc("POST /api/v1/transfers", handler.Transfer)

	// ASSIGNMENT.md Root Compatibility Endpoints
	mux.HandleFunc("POST /wallets", handler.CreateWallet)
	mux.HandleFunc("GET /wallets/{id}", handler.GetWallet)
	mux.HandleFunc("POST /transfers", handler.Transfer)

	return mux
}
