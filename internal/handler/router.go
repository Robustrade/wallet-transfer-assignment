package handler

import "net/http"

// NewRouter wires the HTTP routes and the middleware chain.
//
// Routing relies on Go 1.22+ method-aware ServeMux patterns, which
// give us:
//   - "POST /transfers" — verb-matched route, 405 on wrong verb
//   - "GET /wallets/{id}" — path parameter accessed via r.PathValue("id")
//
// No third-party router is needed.
//
// The mux is wrapped with the middleware chain in this fixed order:
//
//	RequestID  ->  AccessLog  ->  Recover  ->  mux
//
// Rationale (see Chain for the longer version):
//   - RequestID is outermost so an id is in the context before any
//     downstream code logs.
//   - AccessLog is in the middle so it can wrap the ResponseWriter
//     and read the final status (including 500s produced by
//     Recover).
//   - Recover is innermost so it catches panics from the matched
//     handler and writes a generic 500 instead of letting the runtime
//     crash the goroutine.
//
// The /healthz route is wired here (not via a HealthHandler struct)
// because it's stateless and exists only as a liveness probe.
func NewRouter(transfer *TransferHandler, wallet *WalletHandler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transfer.Create)
	mux.HandleFunc("GET /transfers/{id}", transfer.Get)
	mux.HandleFunc("POST /wallets", wallet.Create)
	mux.HandleFunc("GET /wallets/{id}", wallet.Get)
	mux.HandleFunc("GET /wallets/{id}/transfers", wallet.ListTransfers)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	return Chain(mux, RequestID, AccessLog, Recover)
}
