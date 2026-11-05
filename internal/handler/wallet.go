package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// WalletHandler serves /wallets.
type WalletHandler struct {
	svc *service.WalletService
	log *slog.Logger
}

type createWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

// Create handles POST /wallets (demo/test helper to seed wallets).
func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := decodeJSON(r, &req); err != nil {
		writeError(w, r, h.log, err)
		return
	}
	wallet, err := h.svc.CreateWallet(r.Context(), req.ID, req.InitialBalance)
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusCreated, wallet)
}

// Get handles GET /wallets/{id}.
func (h *WalletHandler) Get(w http.ResponseWriter, r *http.Request) {
	wallet, err := h.svc.GetWallet(r.Context(), r.PathValue("id"))
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, wallet)
}

// Ledger handles GET /wallets/{id}/ledger?limit=50.
func (h *WalletHandler) Ledger(w http.ResponseWriter, r *http.Request) {
	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 500 {
			writeError(w, r, h.log, fmt.Errorf("%w: limit must be 1-500", domain.ErrValidation))
			return
		}
		limit = n
	}

	id := r.PathValue("id")
	entries, err := h.svc.GetLedger(r.Context(), id, limit)
	if err != nil {
		writeError(w, r, h.log, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"walletId": id, "entries": entries})
}
