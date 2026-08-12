package httpapi

import (
	"log/slog"
	"net/http"
)

// maxRequestBodyBytes caps every request body this API accepts, so a
// malformed or hostile oversized payload can't hold a connection or decoder
// open indefinitely.
const maxRequestBodyBytes = 64 * 1024

// NewRouter builds the full HTTP handler: routes wrapped in request id
// assignment, structured access logging, and panic recovery. Handlers are
// intentionally thin — see docs/design.md — decode, call the service, map
// the result; no business rules or SQL live in this package.
func NewRouter(transfers TransferService, wallets WalletService, health HealthChecker, logger *slog.Logger) http.Handler {
	mux := http.NewServeMux()

	th := &transfersHandler{transfers: transfers}
	wh := &walletsHandler{wallets: wallets}
	sh := &systemHandler{health: health}

	mux.HandleFunc("POST /transfers", th.Create)
	mux.HandleFunc("GET /transfers/{id}", th.Get)
	mux.HandleFunc("POST /wallets", wh.Create)
	mux.HandleFunc("GET /wallets/{id}", wh.Get)
	mux.HandleFunc("GET /wallets/{id}/entries", wh.ListEntries)
	mux.HandleFunc("GET /healthz", sh.Healthz)
	mux.HandleFunc("GET /readyz", sh.Readyz)

	return withMiddleware(mux, logger)
}
