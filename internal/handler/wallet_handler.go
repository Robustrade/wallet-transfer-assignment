package handler

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"github.com/go-chi/chi/v5"
)

// WalletHandler handles HTTP requests for wallet operations.
type WalletHandler struct {
	walletService *service.WalletService
	logger        *slog.Logger
}

// NewWalletHandler creates a new WalletHandler.
func NewWalletHandler(walletService *service.WalletService, logger *slog.Logger) *WalletHandler {
	return &WalletHandler{
		walletService: walletService,
		logger:        logger,
	}
}

// RegisterRoutes registers wallet routes on the given router.
func (h *WalletHandler) RegisterRoutes(r chi.Router) {
	r.Post("/wallets", h.CreateWallet)
	r.Get("/wallets/{id}", h.GetWallet)
	r.Get("/wallets/{id}/transactions", h.GetTransactionHistory)
}

// CreateWallet handles POST /wallets
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req CreateWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if validationErr := req.Validate(); validationErr != nil {
		writeError(w, http.StatusBadRequest, validationErr.Error())
		return
	}

	wallet, err := h.walletService.CreateWallet(r.Context(), req.ID, req.InitialBalance, req.Currency)
	if err != nil {
		if errors.Is(err, domain.ErrDuplicateWallet) {
			writeError(w, http.StatusConflict, "wallet already exists")
			return
		}
		h.logger.ErrorContext(r.Context(), "failed to create wallet", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, toWalletResponse(wallet))
}

// GetWallet handles GET /wallets/{id}
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "wallet ID is required")
		return
	}

	wallet, err := h.walletService.GetWallet(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, "wallet not found")
			return
		}
		h.logger.ErrorContext(r.Context(), "failed to get wallet", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

// GetTransactionHistory handles GET /wallets/{id}/transactions
func (h *WalletHandler) GetTransactionHistory(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "wallet ID is required")
		return
	}

	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	offset, _ := strconv.Atoi(r.URL.Query().Get("offset"))

	entries, err := h.walletService.GetTransactionHistory(r.Context(), id, limit, offset)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, "wallet not found")
			return
		}
		h.logger.ErrorContext(r.Context(), "failed to get transaction history", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, toTransactionHistoryResponse(entries))
}
