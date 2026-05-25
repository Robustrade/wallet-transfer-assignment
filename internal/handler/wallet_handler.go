package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"

	"github.com/go-chi/chi/v5"
)

type createWalletRequest struct {
	WalletID       *string `json:"walletId"`
	InitialBalance int64   `json:"initialBalance"`
}

type ledgerEntryResponse struct {
	EntryID    string `json:"entryId"`
	TransferID string `json:"transferId"`
	Type       string `json:"type"`
	Amount     int64  `json:"amount"`
}

type walletLedgerResponse struct {
	WalletID string                `json:"walletId"`
	Entries  []ledgerEntryResponse `json:"entries"`
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
			if errors.Is(err, domain.ErrWalletAlreadyExists) {

				http.Error(w, "walletId already exists", http.StatusConflict)
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

func getWalletLedger(transferService *service.TransferService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		walletID := chi.URLParam(r, "walletId")

		entries, err := transferService.ListWalletLedgerEntries(r.Context(), walletID)
		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := walletLedgerResponse{
			WalletID: walletID,
			Entries:  make([]ledgerEntryResponse, 0, len(entries)),
		}
		for _, entry := range entries {
			response.Entries = append(response.Entries, ledgerEntryResponse{
				EntryID:    string(entry.ID),
				TransferID: string(entry.TransferID),
				Type:       string(entry.Type),
				Amount:     entry.Amount,
			})
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("encode response error: %v", err)
		}
	}
}

func getWallet(walletService *service.WalletService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		walletID := chi.URLParam(r, "walletId")

		resp, err := walletService.GetWallet(r.Context(), walletID)

		if err != nil {
			if errors.Is(err, domain.ErrWalletNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		if err := json.NewEncoder(w).Encode(resp); err != nil {
			log.Printf("encode response error: %v", err)
		}
	}
}
