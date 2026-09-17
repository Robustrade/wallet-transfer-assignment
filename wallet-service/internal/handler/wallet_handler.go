package handler

import (
	"net/http"
	"strings"

	"github.com/shubh/wallet-service/internal/domain"
	"github.com/shubh/wallet-service/internal/service"
)

// WalletHandler handles HTTP requests for wallet endpoints.
type WalletHandler struct {
	transferService *service.TransferService
}

// NewWalletHandler creates a new WalletHandler.
func NewWalletHandler(ts *service.TransferService) *WalletHandler {
	return &WalletHandler{transferService: ts}
}

// GetWallet handles GET /wallets/{id}.
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	// Extract wallet ID from path: /wallets/{id}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/wallets/"), "/")
	walletID := parts[0]
	if walletID == "" {
		writeError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	wallet, err := h.transferService.GetWallet(r.Context(), walletID)
	if err != nil {
		if err == domain.ErrWalletNotFound {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}
