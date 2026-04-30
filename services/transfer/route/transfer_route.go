// Package route registers the HTTP routes for the transfer service.
package route

import (
	"log/slog"
	"net/http"

	"github.com/gorilla/mux"

	"github.com/Robustrade/wallet-transfer-assignment/common/middleware"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/handler"
)

// MaxRequestBodyBytes caps the size of any request body. The transfer payload
// is a few hundred bytes; 64 KiB is a generous ceiling.
const MaxRequestBodyBytes int64 = 64 * 1024

// InitRoutes attaches the transfer service routes (and shared middleware)
// to the supplied router.
func InitRoutes(router *mux.Router, h *handler.TransferHandler, logger *slog.Logger) {
	router.Use(middleware.RequestID)
	router.Use(middleware.Recovery(logger))
	router.Use(middleware.Logging(logger))
	router.Use(middleware.MaxBodyBytes(MaxRequestBodyBytes))

	// JSON 404 / 405 instead of gorilla/mux's plain-text defaults.
	//
	// Wrap these explicitly with RequestID: gorilla/mux does NOT run the
	// router's `Use(...)` middleware on `NotFoundHandler` /
	// `MethodNotAllowedHandler`, so without this wrap the RequestID middleware
	// would be skipped and the documented X-Request-ID contract would not hold
	// for unmatched routes / methods.
	router.NotFoundHandler = middleware.RequestID(middleware.NotFoundHandler())
	router.MethodNotAllowedHandler = middleware.RequestID(
		middleware.MethodNotAllowedHandler(allowedMethodsFor(router)),
	)

	router.HandleFunc("/transfers", h.CreateTransfer).Methods(http.MethodPost)
}

// allowedMethodsFor returns a probe that, given a request, asks the router
// which HTTP methods would have matched the same path. Used to populate the
// `Allow` response header on 405 responses (RFC 9110 §10.2.1).
//
// The probe re-runs the matcher with each common method on a request clone, so
// it composes correctly with any path/host/header matchers configured on the
// route — there is no need to inspect path templates by hand.
func allowedMethodsFor(router *mux.Router) func(*http.Request) []string {
	candidates := []string{
		http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut,
		http.MethodPatch, http.MethodDelete, http.MethodOptions,
	}
	return func(r *http.Request) []string {
		var allowed []string
		for _, m := range candidates {
			probe := r.Clone(r.Context())
			probe.Method = m
			var match mux.RouteMatch
			if router.Match(probe, &match) && match.MatchErr == nil {
				allowed = append(allowed, m)
			}
		}
		return allowed
	}
}
