package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/example/wallet-transfer-assignment/internal/domain"
)

type WalletHandler struct {
	service interface {
		GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error)
	}
}

func NewWalletHandler(svc interface {
	GetWallet(ctx context.Context, walletID string) (*domain.Wallet, error)
}) *WalletHandler {
	return &WalletHandler{service: svc}
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletID")
	if walletID == "" {
		respondError(w, http.StatusBadRequest, "wallet id required")
		return
	}

	wallet, err := h.service.GetWallet(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			respondError(w, http.StatusNotFound, "wallet not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "internal error")
		return
	}

	respondJSON(w, http.StatusOK, wallet)
}
