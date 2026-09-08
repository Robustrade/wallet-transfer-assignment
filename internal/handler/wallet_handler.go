package handler

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"wallet-transfer-assignment/internal/service"
)

type WalletHandler struct {
	service *service.WalletService
}

func NewWalletHandler(svc *service.WalletService) *WalletHandler {
	return &WalletHandler{service: svc}
}

type createWalletRequest struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var req createWalletRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", err.Error())
		return
	}

	wallet, err := h.service.CreateWallet(r.Context(), req.ID, req.Balance)
	if err != nil {
		mapWalletError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, wallet)
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSONError(w, http.StatusBadRequest, "INVALID_REQUEST", "wallet id is required")
		return
	}

	wallet, err := h.service.GetWallet(r.Context(), id)
	if err != nil {
		mapWalletError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, wallet)
}
