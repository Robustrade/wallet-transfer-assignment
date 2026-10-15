package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/RKShukla1997/wallet-transfer-assignment/internal/domain"
	"github.com/RKShukla1997/wallet-transfer-assignment/internal/service"
)

// WalletHandler is a thin HTTP adapter for the optional wallet balance API.
type WalletHandler struct {
	service *service.WalletService
}

func NewWalletHandler(s *service.WalletService) *WalletHandler {
	return &WalletHandler{service: s}
}

type walletResponse struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

func toWalletResponse(w domain.Wallet) walletResponse {
	return walletResponse{ID: w.ID, Balance: w.Balance}
}

// Get handles GET /wallets/{id}.
func (h *WalletHandler) Get(w http.ResponseWriter, r *http.Request, id string) {
	wallet, err := h.service.GetWallet(r.Context(), id)
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}
	writeJSON(w, http.StatusOK, toWalletResponse(wallet))
}

type createWalletRequest struct {
	ID              string `json:"id"`
	StartingBalance int64  `json:"startingBalance"`
}

// Create handles POST /wallets. This is a convenience endpoint for
// seeding/demo purposes, not a core transfer requirement.
func (h *WalletHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, errors.New("invalid request body"))
		return
	}
	if req.ID == "" {
		writeError(w, http.StatusBadRequest, errors.New("id is required"))
		return
	}

	wallet, err := h.service.CreateWallet(r.Context(), req.ID, req.StartingBalance)
	if err != nil {
		writeError(w, statusForError(err), err)
		return
	}
	writeJSON(w, http.StatusCreated, toWalletResponse(wallet))
}
