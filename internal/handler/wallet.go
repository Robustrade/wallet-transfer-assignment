package handler

import (
	"errors"
	"log/slog"
	"net/http"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"

	"github.com/go-chi/chi/v5"
)

type createWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

// POST /wallets
func (h *Handler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	var req createWalletRequest
	if err := decodeJSON(r, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, errorResponse{"invalid request body"})
		return
	}

	wallet, err := h.svc.CreateWallet(r.Context(), service.CreateWalletRequest{
		ID:             req.ID,
		InitialBalance: req.InitialBalance,
	})
	if err != nil {
		if errors.Is(err, domain.ErrInvalidAmount) {
			writeJSON(w, http.StatusBadRequest, errorResponse{err.Error()})
			return
		}
		slog.Error("create wallet failed", "error", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{"failed to create wallet"})
		return
	}

	writeJSON(w, http.StatusCreated, wallet)
}

// GET /wallets/{walletId}
func (h *Handler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "walletId")

	wallet, err := h.svc.GetWallet(r.Context(), id)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"wallet not found"})
			return
		}
		slog.Error("get wallet failed", "wallet_id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error"})
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}

// GET /wallets/{walletId}/transfers
func (h *Handler) GetTransferHistory(w http.ResponseWriter, r *http.Request) {
	walletID := chi.URLParam(r, "walletId")

	transfers, err := h.svc.GetTransferHistory(r.Context(), walletID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			writeJSON(w, http.StatusNotFound, errorResponse{"wallet not found"})
			return
		}
		slog.Error("get transfer history failed", "wallet_id", walletID, "error", err)
		writeJSON(w, http.StatusInternalServerError, errorResponse{"internal server error"})
		return
	}

	// Return an empty array rather than null when there are no transfers.
	if transfers == nil {
		transfers = []domain.Transfer{}
	}
	writeJSON(w, http.StatusOK, transfers)
}
