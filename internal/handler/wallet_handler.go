package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	svc service.WalletService
}

func NewWalletHandler(svc service.WalletService) *WalletHandler {
	return &WalletHandler{svc: svc}
}

// GetWalletBalance handles GET /wallets/{id}
func (h *WalletHandler) GetWalletBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	walletID := strings.TrimPrefix(r.URL.Path, "/wallets/")
	if walletID == "" || walletID == r.URL.Path {
		// Just a safeguard if they ping /wallets exactly
		http.Error(w, "wallet ID is required", http.StatusBadRequest)
		return
	}

	wallet, err := h.svc.GetWalletBalance(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			h.respondJSON(w, http.StatusNotFound, map[string]string{"error": "wallet not found"})
			return
		}
		h.respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	// We can choose to return the whole wallet object or just the balance.
	// Returning the wallet object provides `id`, `balance`, `created_at` which is standard.
	h.respondJSON(w, http.StatusOK, wallet)
}

func (h *WalletHandler) respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
