// Package handler implements the HTTP transport layer for the wallet-transfer
// service. Handlers depend only on the service.TransferService interface, which
// keeps them thin and testable with fakes.
package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/ferozyk/wallet-transfer-assignment/internal/service"
)

// New builds the HTTP handler for the service: a chi router with standard
// middleware (request id, real ip, structured logging, panic recovery) and the
// transfer/wallet/health routes mounted on it.
func New(svc service.TransferService) http.Handler {
	h := &transferHandler{svc: svc}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", health)

	r.Route("/transfers", func(r chi.Router) {
		r.Post("/", h.create)
		r.Get("/{id}", h.get)
	})

	r.Route("/wallets", func(r chi.Router) {
		r.Get("/{id}/balance", h.balance)
	})

	return r
}

// health is a liveness probe that always reports ok.
func health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
