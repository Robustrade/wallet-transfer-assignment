// Package httpapi wires the HTTP transport: router, middleware and the
// shared JSON/error helpers used by every handler.
package httpapi

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

// Routes is the set of feature handlers mounted on the router. Each feature
// module registers itself here
type Routes struct {
	Transfer func(r chi.Router) // mounts POST /transfers; nil until wired
	Wallet   func(r chi.Router) // mounts optional GET /wallets/{id}; nil until wired
}

// NewRouter builds the application router with baseline middleware:
// request IDs for traceability and panic recovery
func NewRouter(routes Routes) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	if routes.Transfer != nil {
		routes.Transfer(r)
	}
	if routes.Wallet != nil {
		routes.Wallet(r)
	}
	return r
}

// WriteJSON is the single place response encoding happens
func WriteJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}
