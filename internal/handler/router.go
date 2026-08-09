package handler

import (
	"encoding/json"
	"net/http"
)

// NewRouter constructs an http.Handler registering all wallet transfer routes.
func NewRouter(walletHandler *WalletHandler, transferHandler *TransferHandler) http.Handler {
	mux := http.NewServeMux()

	// Health check endpoint
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "UP"})
	})

	// Wallet endpoints
	mux.HandleFunc("POST /wallets", walletHandler.CreateWallet)
	mux.HandleFunc("GET /wallets/{id}", walletHandler.GetWallet)
	mux.HandleFunc("GET /wallets/{id}/audit", walletHandler.AuditWallet)

	// Transfer endpoints
	mux.HandleFunc("POST /transfers", transferHandler.CreateTransfer)
	mux.HandleFunc("GET /transfers/{id}", transferHandler.GetTransfer)

	return mux
}
