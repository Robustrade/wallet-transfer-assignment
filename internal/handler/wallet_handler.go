package handler

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/skc1234/wallet-transfer-assignment/internal/domain"
	"github.com/skc1234/wallet-transfer-assignment/internal/repository"
)

type WalletHandler struct {
	Repo *repository.WalletRepository
}

func NewWalletHandler(
	repo *repository.WalletRepository,
) *WalletHandler {
	return &WalletHandler{
		Repo: repo,
	}
}

// Create a new wallet
func (h *WalletHandler) CreateWallet(
	w http.ResponseWriter,
	r *http.Request,
) {
	var request struct {
		Balance float64 `json:"balance"`
	}

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	if request.Balance < 0 {
		http.Error(w, "Balance cannot be negative", http.StatusBadRequest)
		return
	}

	walletID, err := h.Repo.CreateWallet(
		r.Context(),
		request.Balance,
	)
	if err != nil {
		http.Error(w, "Failed to create wallet", http.StatusInternalServerError)
		return
	}

	wallet := domain.Wallet{
		ID:        walletID,
		Balance:   request.Balance,
		Currency:  "INR",
		CreatedAt: time.Now(),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(wallet); err != nil {
		return
	}
}

// Get wallet by ID
func (h *WalletHandler) GetWallet(
	w http.ResponseWriter,
	r *http.Request,
) {
	walletID := r.PathValue("id")

	if walletID == "" {
		http.Error(w, "Wallet ID is required", http.StatusBadRequest)
		return
	}

	wallet, err := h.Repo.GetWallet(r.Context(), walletID)
	if err != nil {
		http.Error(w, "Wallet not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(wallet); err != nil {
		return
	}
}
