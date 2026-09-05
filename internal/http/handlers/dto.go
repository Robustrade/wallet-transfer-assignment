package handlers

import (
	"time"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/service"
)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey" binding:"required"`
	FromWalletID   string `json:"fromWalletId"   binding:"required"`
	ToWalletID     string `json:"toWalletId"     binding:"required"`
	Amount         *int64 `json:"amount"         binding:"required"`
}

func (r createTransferRequest) toDomain() domain.TransferRequest {
	var amount domain.Amount
	if r.Amount != nil {
		amount = domain.Amount(*r.Amount)
	}
	return domain.TransferRequest{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         amount,
	}
}

type transferResponse struct {
	TransferID   string    `json:"transferId"`
	Status       string    `json:"status"`
	FromWalletID string    `json:"fromWalletId"`
	ToWalletID   string    `json:"toWalletId"`
	Amount       int64     `json:"amount"`
	Currency     string    `json:"currency"`
	CreatedAt    time.Time `json:"createdAt"`
}

func transferResponseFromResult(r service.TransferResult) transferResponse {
	return transferResponse{
		TransferID:   r.TransferID.String(),
		Status:       string(r.Status),
		FromWalletID: r.FromWalletID,
		ToWalletID:   r.ToWalletID,
		Amount:       int64(r.Amount),
		Currency:     r.Currency,
		CreatedAt:    r.CreatedAt,
	}
}
