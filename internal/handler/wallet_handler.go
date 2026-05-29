
package handler

import (
    "encoding/json"
    "net/http"

    "wallet-transfer-assignment/internal/domain"
    "wallet-transfer-assignment/internal/repository"
)

type WalletHandler struct {
    repo *repository.WalletRepository
}

func NewWalletHandler(repo *repository.WalletRepository) *WalletHandler {
    return &WalletHandler{repo: repo}
}

func (h *WalletHandler) HandleWallets(
    w http.ResponseWriter,
    r *http.Request,
) {

    switch r.Method {

    case http.MethodPost:
        h.createWallet(w, r)

    case http.MethodGet:
        h.getWallet(w, r)

    default:
        http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
    }
}

func (h *WalletHandler) createWallet(
    w http.ResponseWriter,
    r *http.Request,
) {

    var wallet domain.Wallet

    err := json.NewDecoder(r.Body).Decode(&wallet)
    if err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }

    err = h.repo.Create(&wallet)
    if err != nil {
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    w.WriteHeader(http.StatusCreated)

    json.NewEncoder(w).Encode(wallet)
}

func (h *WalletHandler) getWallet(
    w http.ResponseWriter,
    r *http.Request,
) {

    id := r.URL.Query().Get("id")

    wallet, err := h.repo.Get(id)
    if err != nil {
        http.Error(w, "wallet not found", http.StatusNotFound)
        return
    }

    w.Header().Set("Content-Type", "application/json")

    json.NewEncoder(w).Encode(wallet)
}
