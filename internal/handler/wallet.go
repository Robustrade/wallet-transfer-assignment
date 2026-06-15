package handler

import (
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/repository"
)

// WalletHandler exposes read-only wallet endpoints.
type WalletHandler struct {
	repo *repository.WalletRepository
}

func NewWalletHandler(repo *repository.WalletRepository) *WalletHandler {
	return &WalletHandler{repo: repo}
}

type walletResponse struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// GetWallet handles GET /wallets/{walletId}.
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "walletId")
	wallet, err := h.repo.GetByID(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeError(w, http.StatusNotFound, "wallet not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, walletResponse{
		ID:        wallet.ID,
		Balance:   wallet.Balance,
		UpdatedAt: wallet.UpdatedAt,
	})
}
