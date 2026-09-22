// Package router registers HTTP routes and wires them to controller
// handlers with the standard middleware chain applied.
package router

import (
	"log/slog"
	"net/http"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/controller"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/middleware"
)

// New builds the complete HTTP handler for the service, wiring routes to
// handlers and applying cross-cutting middleware.
func New(transfers controller.TransferService, wallets controller.WalletService, logger *slog.Logger) http.Handler {
	transferHandler := controller.NewTransferHandler(transfers, logger)
	walletHandler := controller.NewWalletHandler(wallets, logger)

	mux := http.NewServeMux()
	mux.HandleFunc("POST /transfers", transferHandler.Create)
	mux.HandleFunc("GET /transfers/{id}", transferHandler.Get)
	mux.HandleFunc("POST /wallets", walletHandler.Create)
	mux.HandleFunc("GET /wallets/{id}/balance", walletHandler.GetBalance)
	mux.HandleFunc("GET /wallets/{id}/transfers", walletHandler.ListTransfers)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		controller.WriteJSON(w, logger, http.StatusOK, map[string]string{"status": "ok"})
	})

	var handler http.Handler = mux
	handler = middleware.Logging(logger)(handler)
	handler = middleware.Recover(logger)(handler)
	handler = middleware.RequestID(handler)
	return handler
}
