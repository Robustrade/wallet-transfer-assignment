package handler

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type createTransferRequest struct {
	FromWalletId   string `json:"fromWalletId"`
	ToWalletId     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	IdempotencyKey string `json:"idempotencyKey"`
}
type TransferResponse struct {
	TransferId   string `json:"transferId"`
	Status       string `json:"status"`
	FromWalletId string `json:"fromWalletId"`
	ToWalletId   string `json:"toWalletId"`
	Amount       int64  `json:"amount"`
}

func createTransfer(transferService *service.TransferService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req createTransferRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		if req.IdempotencyKey == "" || req.FromWalletId == "" || req.ToWalletId == "" || req.Amount <= 0 {
			http.Error(w, "invalid request body", http.StatusBadRequest)
			return
		}
		resp, alreadyExists, err := transferService.CreateTransfer(r.Context(),
			service.CreateTransferRequest{
				FromWalletId:   req.FromWalletId,
				ToWalletId:     req.ToWalletId,
				Amount:         req.Amount,
				IdempotencyKey: req.IdempotencyKey,
			},
		)
		if err != nil {

			if errors.Is(err, domain.ErrInvalidAmount) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if errors.Is(err, domain.ErrSameWalletTransfer) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if errors.Is(err, domain.ErrInsufficientFunds) {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if errors.Is(err, domain.ErrWalletNotFound) {
				http.Error(w, err.Error(), http.StatusNotFound)
				return
			}

			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		response := TransferResponse{
			TransferId:   string(resp.ID),
			Status:       string(resp.Status),
			FromWalletId: string(resp.FromWalletID),
			ToWalletId:   string(resp.ToWalletID),
			Amount:       resp.Amount,
		}

		w.Header().Set("Content-Type", "application/json")
		if alreadyExists {
			w.WriteHeader(http.StatusOK)
		} else {
			w.WriteHeader(http.StatusCreated)
		}

		if err := json.NewEncoder(w).Encode(response); err != nil {
			log.Printf("encode response error: %v", err)
		}
	}
}
