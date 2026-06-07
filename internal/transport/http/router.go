package http

import (
	"net/http"
)

// NewRouter wires the API routes onto a net/http ServeMux. Routes are versioned
// under /api/v1 so the contract can evolve without breaking existing clients.
// The Go 1.22+ pattern syntax gives method-aware routing and path variables
// without a third-party router.
func NewRouter(h *Handler) http.Handler {
	mux := http.NewServeMux()

	// Transfers.
	mux.HandleFunc("POST /api/v1/transfers", h.createTransfer)
	mux.HandleFunc("GET /api/v1/transfers/{id}", h.getTransfer)

	// Wallets and their ledger history.
	mux.HandleFunc("POST /api/v1/wallets", h.createWallet)
	mux.HandleFunc("GET /api/v1/wallets/{id}", h.getWallet)
	mux.HandleFunc("GET /api/v1/wallets/{id}/ledger", h.listWalletLedger)

	// Operational endpoint.
	mux.HandleFunc("GET /healthz", h.health)

	// Apply cross-cutting middleware (outermost first).
	return recoverMiddleware(h.logger)(requestLogMiddleware(h.logger)(mux))
}
