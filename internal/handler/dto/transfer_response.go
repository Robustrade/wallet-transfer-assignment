package dto

import (
	"github.com/google/uuid"

	"wallet-transfer-assignment/internal/domain"
)

type CreateTransferResponse struct {
	ID            uuid.UUID `json:"id"`
	FromWalletID  uuid.UUID `json:"fromWalletId"`
	ToWalletID    uuid.UUID `json:"toWalletId"`
	Amount        int64     `json:"amount"`
	Status        string    `json:"status"`
	FailureReason string    `json:"failureReason,omitempty"`
}

func FromTransfer(
	t *domain.Transfer,
) CreateTransferResponse {
	response := CreateTransferResponse{
		ID:           t.ID,
		FromWalletID: t.FromWalletID,
		ToWalletID:   t.ToWalletID,
		Amount:       t.Amount,
		Status:       string(t.Status),
	}

	if t.FailureReason != domain.TransferFailureNone {
		response.FailureReason = string(t.FailureReason)
	}

	return response
}
