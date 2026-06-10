package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/prateekgautam/wallet-transfer-assignment/internal/domain"
	"github.com/prateekgautam/wallet-transfer-assignment/internal/service"
)

// WalletHandler handles HTTP requests for wallet operations.
type WalletHandler struct {
	service *service.TransferService
	logger  *slog.Logger
}

// NewWalletHandler creates a new WalletHandler.
func NewWalletHandler(svc *service.TransferService, logger *slog.Logger) *WalletHandler {
	return &WalletHandler{service: svc, logger: logger}
}

// CreateWallet handles POST /wallets.
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req domain.CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "invalid JSON request body")
		return
	}

	wallet, err := h.service.CreateWallet(r.Context(), req)
	if err != nil {
		handleServiceError(w, h.logger, err)
		return
	}

	writeJSON(w, http.StatusCreated, wallet)
}

// GetWallet handles GET /wallets/{id}.
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "INVALID_INPUT", "wallet ID is required")
		return
	}

	wallet, err := h.service.GetWallet(r.Context(), id)
	if err != nil {
		handleServiceError(w, h.logger, err)
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}
