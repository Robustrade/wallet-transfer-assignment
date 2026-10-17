package handler

import (
	"net/http"
	"strings"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// WalletHandler handles wallet balance queries.
type WalletHandler struct {
	transferService service.TransferService
}

// NewWalletHandler constructs a new WalletHandler.
func NewWalletHandler(transferService service.TransferService) *WalletHandler {
	return &WalletHandler{transferService: transferService}
}

// HandleGetBalance processes GET /wallets/{id}/balance
func (h *WalletHandler) HandleGetBalance(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		WriteError(w, domain.NewDomainError(domain.CodeInvalidRequest, "method not allowed"))
		return
	}

	walletID := r.PathValue("id")
	if walletID == "" {
		// Fallback for custom routing
		path := strings.TrimPrefix(r.URL.Path, "/wallets/")
		walletID = strings.TrimSuffix(path, "/balance")
	}
	walletID = strings.TrimSpace(walletID)
	if walletID == "" {
		WriteError(w, domain.NewDomainError(domain.CodeInvalidRequest, "wallet id is required"))
		return
	}

	balanceResult, err := h.transferService.GetWalletBalance(r.Context(), walletID)
	if err != nil {
		WriteError(w, err)
		return
	}

	WriteJSON(w, http.StatusOK, balanceResult)
}
