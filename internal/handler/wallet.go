package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

type WalletHandler struct {
	repo *repository.WalletRepository
}

func NewWalletHandler(repo *repository.WalletRepository) *WalletHandler {
	return &WalletHandler{repo: repo}
}

type createWalletRequest struct {
	InitialBalance int64 `json:"initialBalance"`
}

type walletResponse struct {
	ID        string `json:"id"`
	Balance   int64  `json:"balance"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.InitialBalance < 0 {
		writeError(w, http.StatusBadRequest, "initialBalance must be non-negative")
		return
	}

	now := time.Now().UTC()
	wallet := &domain.Wallet{
		ID:        uuid.New().String(),
		Balance:   req.InitialBalance,
		CreatedAt: now,
		UpdatedAt: now,
	}
	if err := h.repo.Create(r.Context(), wallet); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusCreated, toWalletResponse(wallet))
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	wallet, err := h.repo.GetByID(r.Context(), h.repo.DB(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, "wallet not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

func toWalletResponse(w *domain.Wallet) walletResponse {
	return walletResponse{
		ID:        w.ID,
		Balance:   w.Balance,
		CreatedAt: w.CreatedAt.Format(time.RFC3339),
		UpdatedAt: w.UpdatedAt.Format(time.RFC3339),
	}
}
