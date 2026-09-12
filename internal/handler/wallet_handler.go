package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	walletService *service.WalletService
	adminKey      string
}

func NewWalletHandler(walletService *service.WalletService, adminKey string) *WalletHandler {
	return &WalletHandler{
		walletService: walletService,
		adminKey:      adminKey,
	}
}

func (h *WalletHandler) isAuthorizedAdmin(r *http.Request) bool {
	if h.adminKey == "" {
		return false
	}
	if r.Header.Get("X-Admin-Key") == h.adminKey {
		return true
	}
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") && strings.TrimPrefix(auth, "Bearer ") == h.adminKey {
		return true
	}
	return false
}

// CreateWallet handles POST /wallets
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req service.CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	// Initial funding from treasury is strictly restricted on the public router.
	// Unauthenticated callers cannot seed positive initial balances.
	if req.InitialBalance > 0 && !h.isAuthorizedAdmin(r) {
		WriteError(w, http.StatusForbidden, domain.ErrUnauthorizedFunding.Error())
		return
	}

	ctx := r.Context()
	if !h.isAuthorizedAdmin(r) {
		ctx = service.ContextWithPublicCaller(ctx)
	}

	wallet, err := h.walletService.CreateWallet(ctx, req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrWalletNameRequired),
			errors.Is(err, domain.ErrInvalidWalletName),
			errors.Is(err, domain.ErrInvalidWalletID),
			errors.Is(err, domain.ErrInvalidCurrency),
			errors.Is(err, domain.ErrNegativeBalance),
			errors.Is(err, domain.ErrCurrencyMismatch),
			errors.Is(err, domain.ErrBalanceOverflow):
			WriteError(w, http.StatusBadRequest, err.Error())
			return

		case errors.Is(err, domain.ErrUnauthorizedFunding):
			WriteError(w, http.StatusForbidden, err.Error())
			return

		case errors.Is(err, domain.ErrInsufficientFunds):
			WriteError(w, http.StatusUnprocessableEntity, err.Error())
			return

		case errors.Is(err, domain.ErrWalletAlreadyExists):
			WriteError(w, http.StatusConflict, err.Error())
			return

		default:
			log.Printf("[ERROR] failed to create wallet: %v", err)
			WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	WriteJSON(w, http.StatusCreated, wallet)
}

// CreateAdminWallet handles POST /admin/wallets
func (h *WalletHandler) CreateAdminWallet(w http.ResponseWriter, r *http.Request) {
	if !h.isAuthorizedAdmin(r) {
		WriteError(w, http.StatusUnauthorized, "unauthorized: missing or invalid admin key")
		return
	}

	var req service.CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	wallet, err := h.walletService.CreateWallet(r.Context(), req)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrWalletNameRequired),
			errors.Is(err, domain.ErrInvalidWalletName),
			errors.Is(err, domain.ErrInvalidWalletID),
			errors.Is(err, domain.ErrInvalidCurrency),
			errors.Is(err, domain.ErrNegativeBalance),
			errors.Is(err, domain.ErrCurrencyMismatch),
			errors.Is(err, domain.ErrBalanceOverflow):
			WriteError(w, http.StatusBadRequest, err.Error())
			return

		case errors.Is(err, domain.ErrInsufficientFunds):
			WriteError(w, http.StatusUnprocessableEntity, err.Error())
			return

		case errors.Is(err, domain.ErrWalletAlreadyExists):
			WriteError(w, http.StatusConflict, err.Error())
			return

		default:
			log.Printf("[ERROR] failed to create admin wallet: %v", err)
			WriteError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	}

	WriteJSON(w, http.StatusCreated, wallet)
}

// GetWallet handles GET /wallets/{wallet_id}
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	wallet_id := r.PathValue("wallet_id")
	if wallet_id == "" {
		WriteError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	wallet, err := h.walletService.GetWallet(r.Context(), wallet_id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			WriteError(w, http.StatusNotFound, "wallet not found")
			return
		}
		log.Printf("[ERROR] failed to get wallet %s: %v", wallet_id, err)
		WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	WriteJSON(w, http.StatusOK, wallet)
}

// GetWalletLedger handles GET /wallets/{wallet_id}/ledger
func (h *WalletHandler) GetWalletLedger(w http.ResponseWriter, r *http.Request) {
	wallet_id := r.PathValue("wallet_id")
	if wallet_id == "" {
		WriteError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	limit := 50
	offset := 0
	if lStr := r.URL.Query().Get("limit"); lStr != "" {
		l, err := strconv.Atoi(lStr)
		if err != nil || l <= 0 {
			WriteError(w, http.StatusBadRequest, "invalid limit: must be a positive integer")
			return
		}
		if l > 100 {
			l = 100
		}
		limit = l
	}
	if oStr := r.URL.Query().Get("offset"); oStr != "" {
		o, err := strconv.Atoi(oStr)
		if err != nil || o < 0 {
			WriteError(w, http.StatusBadRequest, "invalid offset: must be a non-negative integer")
			return
		}
		offset = o
	}

	entries, err := h.walletService.GetWalletLedger(r.Context(), wallet_id, limit, offset)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			WriteError(w, http.StatusNotFound, "wallet not found")
			return
		}
		log.Printf("[ERROR] failed to get ledger for wallet %s: %v", wallet_id, err)
		WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if entries == nil {
		entries = []domain.LedgerEntry{}
	}

	WriteJSON(w, http.StatusOK, entries)
}

// ReconcileWallet handles GET /wallets/{wallet_id}/reconcile
func (h *WalletHandler) ReconcileWallet(w http.ResponseWriter, r *http.Request) {
	wallet_id := r.PathValue("wallet_id")
	if wallet_id == "" {
		WriteError(w, http.StatusBadRequest, "wallet id is required")
		return
	}

	storedBal, ledgerBal, isBalanced, err := h.walletService.ReconcileBalance(r.Context(), wallet_id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			WriteError(w, http.StatusNotFound, "wallet not found")
			return
		}
		log.Printf("[ERROR] failed to reconcile balance for wallet %s: %v", wallet_id, err)
		WriteError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"walletId":      wallet_id,
		"storedBalance": storedBal,
		"ledgerBalance": ledgerBal,
		"isBalanced":    isBalanced,
	})
}
