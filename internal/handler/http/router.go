// Package http wires the transport layer: request decoding, validation,
// invoking the services, and mapping domain errors to HTTP responses. It
// contains no business logic.
package http

import (
	"log/slog"
	"net/http"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/handler/middleware"
)

// Deps are the collaborators the router needs.
type Deps struct {
	Transfers TransferCreator
	Wallets   WalletReader
	DB        Pinger
	Logger    *slog.Logger
}

// NewRouter builds the application's http.Handler.
func NewRouter(d Deps) http.Handler {
	log := d.Logger
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}

	transfers := NewTransferHandler(d.Transfers, log)
	wallets := NewWalletHandler(d.Wallets, log)
	health := NewHealthHandler(d.DB)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transfers.Create)
	mux.HandleFunc("GET /wallets/{id}/balance", wallets.Balance)
	mux.HandleFunc("GET /healthz", health.Live)
	mux.HandleFunc("GET /readyz", health.Ready)

	return middleware.Chain(mux,
		middleware.Recoverer(log),
		middleware.RequestLogger(log),
	)
}
