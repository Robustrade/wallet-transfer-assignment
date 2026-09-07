package handler

import (
	"encoding/json"
	"net/http"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/service"
)

type WalletHandler struct {
	wallets *service.WalletService
}

func NewWalletHandler(wallets *service.WalletService) *WalletHandler {
	return &WalletHandler{wallets: wallets}
}

type createWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

type walletResponse struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

// CreateWallet handles POST /wallets.
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, domain.ErrInvalidRequest)
		return
	}
	wallet, err := h.wallets.CreateWallet(r.Context(), req.ID, req.InitialBalance)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, walletResponse{ID: wallet.ID, Balance: wallet.Balance})
}

// GetWallet handles GET /wallets/{id} and returns the current stored balance.
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wallet, err := h.wallets.GetWallet(r.Context(), id)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, walletResponse{ID: wallet.ID, Balance: wallet.Balance})
}
