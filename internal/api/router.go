// Package api is the HTTP layer: routing, strict request decoding, and
// status/error mapping. Business logic lives in the service package; handlers
// stay thin.
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strings"

	"wallet/internal/constants"
	"wallet/internal/dto"
	"wallet/internal/service"
)

// NewRouter wires all routes. health is the readiness probe (typically
// store.Ping); it may be nil to skip the database check.
//
// Every route is registered at its canonical /v1 path and at the unversioned
// legacy alias. Each registration is paired with a method-free fallback.
// ServeMux picks the more specific pattern, so the method-qualified route
// handles its own method while every other method gets a JSON 405 with an
// Allow header instead of ServeMux's plain-text default. Unmatched paths get a
// JSON 404 the same way.
func NewRouter(svc *service.Service, logger *slog.Logger, health func(context.Context) error) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &handler{svc: svc, logger: logger}

	routes := []struct {
		method  string
		pattern string
		handler http.HandlerFunc
	}{
		{http.MethodPost, "/transfers", h.postTransfer},
		{http.MethodGet, "/transfers/{id}", h.getTransfer},
		{http.MethodGet, "/wallets/{id}", h.getWallet},
		{http.MethodGet, "/wallets/{id}/ledger", h.listLedger},
		{http.MethodGet, "/healthz", h.healthz(health)},
	}

	mux := http.NewServeMux()
	for _, prefix := range []string{"", "/v1"} {
		allowed := make(map[string][]string, len(routes))
		for _, route := range routes {
			pattern := prefix + route.pattern
			mux.HandleFunc(route.method+" "+pattern, route.handler)
			allowed[pattern] = append(allowed[pattern], route.method)
			if route.method == http.MethodGet {
				// ServeMux serves HEAD through GET routes, so HEAD is allowed
				// wherever GET is; the Allow header says so.
				allowed[pattern] = append(allowed[pattern], http.MethodHead)
			}
		}
		for pattern, methods := range allowed {
			mux.HandleFunc(pattern, methodNotAllowed(strings.Join(methods, ", ")))
		}
	}
	mux.HandleFunc("/", notFound)

	return withRequestLogging(logger, withRecovery(logger, mux))
}

// notFound answers a request whose path matches no route.
func notFound(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusNotFound,
		dto.NewErrorResponse(constants.CodeNotFound, "no route matches this request"))
}

// methodNotAllowed answers a known path with an unsupported method and
// advertises the supported methods in the Allow header.
func methodNotAllowed(allow string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Allow", allow)
		writeJSON(w, http.StatusMethodNotAllowed,
			dto.NewErrorResponse(constants.CodeMethodNotAllowed, "method not allowed; allowed: "+allow))
	}
}

// handler carries the dependencies shared by all HTTP handlers.
type handler struct {
	svc    *service.Service
	logger *slog.Logger
}

func (h *handler) healthz(health func(context.Context) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if health != nil {
			ctx, cancel := context.WithTimeout(r.Context(), constants.HealthTimeout)
			defer cancel()
			if err := health(ctx); err != nil {
				h.logger.ErrorContext(ctx, "health check failed", "error", err)
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
				return
			}
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
