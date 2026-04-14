package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
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

	if strings.HasSuffix(r.URL.Path, "/transfers") {
		h.GetWalletTransfers(w, r)
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
		if errors.Is(err, service.ErrWalletNotFound) {
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

func (h *WalletHandler) GetWalletTransfers(w http.ResponseWriter, r *http.Request) {
	// Path is /wallets/{id}/transfers
	parts := strings.Split(r.URL.Path, "/")
	if len(parts) < 4 {
		http.Error(w, "invalid path", http.StatusBadRequest)
		return
	}
	walletID := parts[2]
	if walletID == "" {
		http.Error(w, "wallet ID is required", http.StatusBadRequest)
		return
	}

	transfers, err := h.svc.GetWalletTransfers(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, service.ErrWalletNotFound) {
			h.respondJSON(w, http.StatusNotFound, map[string]string{"error": "wallet not found"})
			return
		}
		h.respondJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal server error"})
		return
	}

	if transfers == nil {
		transfers = []*model.Transfer{} // Emits [] rather than null
	}
	h.respondJSON(w, http.StatusOK, transfers)
}

func (h *WalletHandler) respondJSON(w http.ResponseWriter, status int, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(payload)
}
