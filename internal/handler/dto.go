package handler

import (
	"time"

	"github.com/jaiminbhaduri/wallet-transfer-assignment/internal/domain"
)

type createTransferRequestDTO struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponseDTO struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Status         string    `json:"status"`
	FailureReason  *string   `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

func toTransferResponseDTO(t *domain.Transfer) transferResponseDTO {
	return transferResponseDTO{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		Status:         string(t.Status),
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
}

type walletResponseDTO struct {
	ID      string `json:"id"`
	Balance int64  `json:"balance"`
}

func toWalletResponseDTO(w *domain.Wallet) walletResponseDTO {
	return walletResponseDTO{ID: w.ID, Balance: w.Balance}
}

type errorResponseDTO struct {
	Error string `json:"error"`
}
