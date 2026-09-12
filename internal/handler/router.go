package handler

import (
	"log"
	"net/http"
	"time"
)

// Config holds handler dependencies for router construction.
type Config struct {
	TransferHandler *TransferHandler
	WalletHandler   *WalletHandler
}

// NewRouter registers HTTP routes and wraps them with middleware.
func NewRouter(cfg Config) http.Handler {
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// Transfers
	mux.HandleFunc("POST /transfers", cfg.TransferHandler.CreateTransfer)
	mux.HandleFunc("GET /transfers/{transfer_id}", cfg.TransferHandler.GetTransfer)

	// Wallets
	mux.HandleFunc("POST /wallets", cfg.WalletHandler.CreateWallet)
	mux.HandleFunc("POST /admin/wallets", cfg.WalletHandler.CreateAdminWallet)
	mux.HandleFunc("GET /wallets/{wallet_id}", cfg.WalletHandler.GetWallet)
	mux.HandleFunc("GET /wallets/{wallet_id}/ledger", cfg.WalletHandler.GetWalletLedger)
	mux.HandleFunc("GET /wallets/{wallet_id}/reconcile", cfg.WalletHandler.ReconcileWallet)

	return recoveryMiddleware(loggingMiddleware(mux))
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("%s %s %s", r.Method, r.URL.Path, time.Since(start))
	})
}

func recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("[PANIC RECOVERED] %v", rec)
				WriteError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
