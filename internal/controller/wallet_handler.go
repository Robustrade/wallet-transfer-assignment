package controller

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/models"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
)

// WalletHandler exposes the wallet HTTP endpoints.
type WalletHandler struct {
	wallets WalletService
	logger  *slog.Logger
}

// NewWalletHandler constructs a WalletHandler.
func NewWalletHandler(wallets WalletService, logger *slog.Logger) *WalletHandler {
	return &WalletHandler{wallets: wallets, logger: logger}
}

// Create handles POST /wallets.
func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req models.CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, h.logger, http.StatusBadRequest, "invalid_json", "request body must be valid JSON")
		return
	}

	wallet, err := h.wallets.CreateWallet(r.Context(), service.CreateWalletInput{ID: req.ID, InitialBalance: req.InitialBalance})
	if err != nil {
		writeDomainError(w, h.logger, err)
		return
	}
	WriteJSON(w, h.logger, http.StatusCreated, models.ToWalletResponse(*wallet))
}

// GetBalance handles GET /wallets/{id}/balance.
func (h *WalletHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wallet, err := h.wallets.GetWallet(r.Context(), id)
	if err != nil {
		writeDomainError(w, h.logger, err)
		return
	}
	WriteJSON(w, h.logger, http.StatusOK, models.ToWalletResponse(*wallet))
}

// ListTransfers handles GET /wallets/{id}/transfers.
func (h *WalletHandler) ListTransfers(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	transfers, err := h.wallets.ListTransferHistory(r.Context(), id)
	if err != nil {
		writeDomainError(w, h.logger, err)
		return
	}
	resp := make([]models.TransferResponse, 0, len(transfers))
	for _, t := range transfers {
		resp = append(resp, models.ToTransferResponse(t, nil))
	}
	WriteJSON(w, h.logger, http.StatusOK, resp)
}
