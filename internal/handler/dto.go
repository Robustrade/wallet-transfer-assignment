package handler

import (
	"time"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/robustrade/wallet-transfer-assignment/internal/service"
)

type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	FromWalletID   string `json:"fromWalletId" binding:"required"`
	ToWalletID     string `json:"toWalletId" binding:"required"`
	Amount         int64  `json:"amount" binding:"required,gt=0"`
}

type TransferResponse struct {
	TransferID     string    `json:"transferId"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Status         string    `json:"status"`
	FailureReason  string    `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
}

func toTransferResponse(r *service.TransferResult) TransferResponse {
	return TransferResponse{
		TransferID:     r.TransferID,
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         r.Amount,
		Status:         string(r.Status),
		FailureReason:  r.FailureReason,
		CreatedAt:      r.CreatedAt,
	}
}

type CreateWalletRequest struct {
	ID       string `json:"id" binding:"required"`
	Balance  int64  `json:"balance" binding:"gte=0"`
	Currency string `json:"currency"`
}

type WalletResponse struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	Currency  string    `json:"currency"`
	CreatedAt time.Time `json:"createdAt,omitempty"`
}

func toWalletResponse(w *service.WalletResult) WalletResponse {
	return WalletResponse{ID: w.ID, Balance: w.Balance, Currency: w.Currency, CreatedAt: w.CreatedAt}
}

type LedgerEntryResponse struct {
	ID         string    `json:"id"`
	TransferID string    `json:"transferId"`
	WalletID   string    `json:"walletId"`
	EntryType  string    `json:"entryType"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}

func toLedgerEntryResponse(e domain.LedgerEntry) LedgerEntryResponse {
	return LedgerEntryResponse{
		ID:         e.ID,
		TransferID: e.TransferID,
		WalletID:   e.WalletID,
		EntryType:  string(e.EntryType),
		Amount:     e.Amount,
		CreatedAt:  e.CreatedAt,
	}
}

type ErrorResponse struct {
	Error string `json:"error"`
}
