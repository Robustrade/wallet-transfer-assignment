package http

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
)

// WalletReader is the handler's view of the wallet service.
type WalletReader interface {
	Balance(ctx context.Context, walletID string) (*domain.Wallet, error)
}

// WalletHandler serves the /wallets endpoints.
type WalletHandler struct {
	svc WalletReader
	log *slog.Logger
}

// NewWalletHandler wires the handler.
func NewWalletHandler(svc WalletReader, log *slog.Logger) *WalletHandler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &WalletHandler{svc: svc, log: log}
}

// Balance handles GET /wallets/{id}/balance.
func (h *WalletHandler) Balance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, h.log, fmt.Errorf("%w: wallet id is required", domain.ErrValidation))
		return
	}
	wallet, err := h.svc.Balance(r.Context(), id)
	if err != nil {
		writeError(w, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, toWalletBalanceResponse(wallet))
}
