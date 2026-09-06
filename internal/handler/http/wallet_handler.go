package http

import (
	"encoding/json"
	"net/http"

	"wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	wallets *service.WalletService
}

func NewWalletHandler(wallets *service.WalletService) *WalletHandler {
	return &WalletHandler{wallets: wallets}
}

// Create is a setup/demo convenience endpoint, not part of the core
// assignment contract: it lets callers open a wallet with a starting
// balance so transfers have something to move money between.
func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "id is required and body must be valid JSON")
		return
	}

	wallet, err := h.wallets.CreateWallet(r.Context(), req.ID, req.InitialBalance)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, walletResponse{ID: wallet.ID, Balance: wallet.Balance, UpdatedAt: wallet.UpdatedAt})
}

func (h *WalletHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")

	wallet, err := h.wallets.GetWallet(r.Context(), id)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, walletResponse{ID: wallet.ID, Balance: wallet.Balance, UpdatedAt: wallet.UpdatedAt})
}
