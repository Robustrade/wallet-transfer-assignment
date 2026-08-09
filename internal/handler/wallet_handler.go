package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/shanks/wallet-transfer-assignment/internal/domain"
	"github.com/shanks/wallet-transfer-assignment/internal/service"
)

// WalletHandler handles HTTP endpoints for wallets.
type WalletHandler struct {
	walletSvc *service.WalletService
}

// NewWalletHandler constructs a WalletHandler instance.
func NewWalletHandler(walletSvc *service.WalletService) *WalletHandler {
	return &WalletHandler{walletSvc: walletSvc}
}

// CreateWalletRequest defines the JSON request payload for creating a wallet.
type CreateWalletRequest struct {
	ID             string `json:"id,omitempty"`
	InitialBalance int64  `json:"initialBalance"`
	Currency       string `json:"currency,omitempty"`
}

// CreateWallet handles POST /wallets requests.
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON payload")
		return
	}

	wallet, err := h.walletSvc.CreateWallet(r.Context(), service.CreateWalletInput{
		ID:             req.ID,
		InitialBalance: req.InitialBalance,
		Currency:       req.Currency,
	})
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err.Error())
		return
	}

	writeJSONResponse(w, http.StatusCreated, wallet)
}

// GetWallet handles GET /wallets/{id} requests.
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	walletID := r.PathValue("id")
	if walletID == "" {
		walletID = r.URL.Query().Get("id")
	}
	if walletID == "" {
		writeJSONError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	wallet, err := h.walletSvc.GetWallet(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to retrieve wallet")
		return
	}

	writeJSONResponse(w, http.StatusOK, wallet)
}

// AuditWallet handles GET /wallets/{id}/audit requests.
func (h *WalletHandler) AuditWallet(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	walletID := r.PathValue("id")
	if walletID == "" {
		walletID = r.URL.Query().Get("id")
	}
	if walletID == "" {
		writeJSONError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	audit, err := h.walletSvc.AuditWalletBalance(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSONError(w, http.StatusNotFound, err.Error())
			return
		}
		writeJSONError(w, http.StatusInternalServerError, "failed to audit wallet balance")
		return
	}

	writeJSONResponse(w, http.StatusOK, audit)
}

func writeJSONResponse(w http.ResponseWriter, statusCode int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)
	_ = json.NewEncoder(w).Encode(data)
}

func writeJSONError(w http.ResponseWriter, statusCode int, message string) {
	writeJSONResponse(w, statusCode, map[string]string{"error": message})
}
