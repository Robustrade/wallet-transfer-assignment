package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type createWalletRequest struct {
	WalletID       *string `json:"walletId"`
	InitialBalance int64   `json:"initialBalance"`
}

func createWallet(walletService *service.WalletService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createWalletRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.WalletID != nil && *req.WalletID == "" {
			http.Error(w, "walletId cannot be empty", http.StatusBadRequest)
			return
		}

		var walletID domain.WalletID
		if req.WalletID != nil {
			walletID = domain.WalletID(*req.WalletID)
		}

		resp, err := walletService.CreateWallet(r.Context(), service.CreateWalletRequest{
			WalletID:       walletID,
			InitialBalance: req.InitialBalance,
		})
		if err != nil {
			if errors.Is(err, domain.ErrInvalidAmount) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("encode response error: %v", err)
		}
	}
}
