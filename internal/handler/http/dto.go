package http

import (
	"time"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

// createTransferRequest is the POST /transfers body.
type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
	Currency       string `json:"currency,omitempty"`
}

func (r createTransferRequest) toCommand() service.CreateTransferCommand {
	return service.CreateTransferCommand{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         r.Amount,
		Currency:       r.Currency,
	}
}

// transferResponse is the transfer resource representation.
type transferResponse struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	State          string    `json:"state"`
	FailureReason  string    `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt,omitempty"`
}

func toTransferResponse(t *domain.Transfer) transferResponse {
	return transferResponse{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount.Amount,
		Currency:       t.Amount.Currency,
		State:          string(t.State),
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
	}
}

// walletBalanceResponse is the GET /wallets/{id}/balance body.
type walletBalanceResponse struct {
	WalletID string `json:"walletId"`
	Balance  int64  `json:"balance"`
	Currency string `json:"currency"`
}

func toWalletBalanceResponse(w *domain.Wallet) walletBalanceResponse {
	return walletBalanceResponse{
		WalletID: w.ID,
		Balance:  w.Balance.Amount,
		Currency: w.Balance.Currency,
	}
}

// errorResponse is the uniform error body.
type errorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code"`
}
