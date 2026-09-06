package handler

import (
	"encoding/json"
	"errors"
	"net/http"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"

	"github.com/google/uuid"
)

// WalletHandler exposes minimal supporting endpoints (create/get wallet) so
// the transfer API can be exercised end-to-end via curl and by tests. These
// are not part of the core assignment requirements but are needed to set up
// wallets to transfer between.
type WalletHandler struct {
	repo repository.WalletRepository
}

func NewWalletHandler(repo repository.WalletRepository) *WalletHandler {
	return &WalletHandler{repo: repo}
}

type createWalletRequestBody struct {
	Name           string      `json:"name"`
	InitialBalance json.Number `json:"initialBalance"`
}

type walletResponseBody struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Balance string `json:"balance"`
}

func (h *WalletHandler) CreateWallet(w http.ResponseWriter, r *http.Request) {
	var body createWalletRequestBody
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "request body must be valid JSON")
		return
	}
	if body.Name == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "name is required")
		return
	}

	initial := body.InitialBalance.String()
	if initial == "" {
		initial = "0"
	}
	cents, err := domain.NumericToCents(initial)
	if err != nil || cents < 0 {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "initialBalance must be a non-negative number with at most 2 decimal places")
		return
	}

	wallet, err := h.repo.CreateWallet(r.Context(), body.Name, cents)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred")
		return
	}

	writeJSON(w, http.StatusCreated, walletResponseBody{
		ID:      wallet.ID.String(),
		Name:    wallet.Name,
		Balance: domain.CentsToNumeric(wallet.Balance),
	})
}

func (h *WalletHandler) GetWallet(w http.ResponseWriter, r *http.Request, idParam string) {
	id, err := uuid.Parse(idParam)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST", "wallet id must be a valid UUID")
		return
	}

	wallet, err := h.repo.GetWallet(r.Context(), id)
	if errors.Is(err, domain.ErrWalletNotFound) {
		writeError(w, http.StatusNotFound, "WALLET_NOT_FOUND", "wallet not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR", "an unexpected error occurred")
		return
	}

	writeJSON(w, http.StatusOK, walletResponseBody{
		ID:      wallet.ID.String(),
		Name:    wallet.Name,
		Balance: domain.CentsToNumeric(wallet.Balance),
	})
}
