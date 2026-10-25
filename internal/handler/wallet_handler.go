package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	walletService *service.WalletService
}

func NewWalletHandler(walletService *service.WalletService) *WalletHandler {
	return &WalletHandler{walletService: walletService}
}

// CreateWallet handles POST /wallets
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req service.CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "invalid json: "+err.Error())
		return
	}

	wallet, err := h.walletService.CreateWallet(r.Context(), req)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "CREATION_FAILED", err.Error())
		return
	}

	WriteJSON(w, http.StatusCreated, wallet)
}

// GetWallet handles GET /wallets/{id} with optional ledger reconciliation
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 2 || parts[1] == "" {
		WriteError(w, http.StatusBadRequest, "BAD_REQUEST", "wallet id is required")
		return
	}
	id := parts[1]

	// Optional query param: ?reconcile=true
	if r.URL.Query().Get("reconcile") == "true" {
		recon, err := h.walletService.ReconcileWallet(r.Context(), id)
		if err != nil {
			if errors.Is(err, model.ErrWalletNotFound) {
				WriteError(w, http.StatusNotFound, "NOT_FOUND", "wallet not found")
				return
			}
			WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
			return
		}
		WriteJSON(w, http.StatusOK, recon)
		return
	}

	wallet, err := h.walletService.GetWallet(r.Context(), id)
	if err != nil {
		if errors.Is(err, model.ErrWalletNotFound) {
			WriteError(w, http.StatusNotFound, "NOT_FOUND", "wallet not found")
			return
		}
		WriteError(w, http.StatusInternalServerError, "INTERNAL_ERROR", err.Error())
		return
	}

	WriteJSON(w, http.StatusOK, wallet)
}
