package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"wallettransfer/internal/domain"
)

// walletCreator/Getter are narrow interfaces so this handler doesn't need
// the full service.WalletRepo surface — just enough for the two convenience
// endpoints below, which exist to make the API testable end-to-end without
// hand-seeding the database.
type walletCreator interface {
	Create(ctx context.Context, walletID string, openingBalance int64) (domain.Wallet, error)
}
type walletGetter interface {
	Get(ctx context.Context, walletID string) (domain.Wallet, error)
}

type WalletHandler struct {
	creator walletCreator
	getter  walletGetter
}

func NewWalletHandler(creator walletCreator, getter walletGetter) *WalletHandler {
	return &WalletHandler{creator: creator, getter: getter}
}

type createWalletRequest struct {
	ID             string `json:"id"`
	OpeningBalance int64  `json:"openingBalance"`
}

type walletResponse struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

// CreateWallet handles POST /wallets. Not part of the core assignment
// requirements, but necessary to open wallets with a starting balance so
// transfers between them can be demonstrated/tested.
func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.ID == "" {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "id is required and body must be valid JSON"})
		return
	}
	if req.OpeningBalance < 0 {
		writeJSON(w, http.StatusBadRequest, errorResponse{Error: "openingBalance must be >= 0"})
		return
	}
	wallet, err := h.creator.Create(r.Context(), req.ID, req.OpeningBalance)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errorResponse{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, walletResponse{ID: wallet.ID, Balance: wallet.Balance})
}

// GetWallet handles GET /wallets/{id}.
func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	wallet, err := h.getter.Get(r.Context(), id)
	if errors.Is(err, domain.ErrWalletNotFound) {
		writeJSON(w, http.StatusNotFound, errorResponse{Error: "wallet not found"})
		return
	}
	if err != nil {

		writeJSON(w, http.StatusInternalServerError,
			errorResponse{Error: "internal server error"})
		return
	}
	writeJSON(w, http.StatusOK, walletResponse{ID: wallet.ID, Balance: wallet.Balance})
}
