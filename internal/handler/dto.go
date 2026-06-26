package handler

import (
	"time"

	"github.com/ferozyk/wallet-transfer-assignment/internal/domain"
	"github.com/ferozyk/wallet-transfer-assignment/internal/service"
)

// transferRequest is the JSON body accepted by POST /transfers.
type transferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// toInput maps the wire request onto the transport-agnostic service command.
func (r transferRequest) toInput() service.TransferInput {
	return service.TransferInput{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         r.Amount,
	}
}

// transferResponse is the JSON representation of a transfer.
type transferResponse struct {
	Id            string    `json:"id"`
	Status        string    `json:"status"`
	FromWalletId  string    `json:"fromWalletId"`
	ToWalletId    string    `json:"toWalletId"`
	Amount        int64     `json:"amount"`
	FailureReason string    `json:"failureReason,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// newTransferResponse maps a domain transfer onto its wire representation.
func newTransferResponse(t *domain.Transfer) transferResponse {
	return transferResponse{
		Id:            t.ID,
		Status:        string(t.Status),
		FromWalletId:  t.FromWalletID,
		ToWalletId:    t.ToWalletID,
		Amount:        int64(t.Amount),
		FailureReason: t.FailureReason,
		CreatedAt:     t.CreatedAt,
		UpdatedAt:     t.UpdatedAt,
	}
}

// walletBalanceResponse is the JSON representation of a wallet balance.
type walletBalanceResponse struct {
	WalletId string `json:"walletId"`
	Balance  int64  `json:"balance"`
}

// newWalletBalanceResponse maps a service balance read model onto the wire type.
func newWalletBalanceResponse(b *service.WalletBalance) walletBalanceResponse {
	return walletBalanceResponse{
		WalletId: b.WalletID,
		Balance:  int64(b.Balance),
	}
}
