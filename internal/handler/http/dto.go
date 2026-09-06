package http

import (
	"time"

	"wallet-transfer-assignment/internal/domain"
)

type createTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

type transferResponse struct {
	TransferID    string    `json:"transferId"`
	Status        string    `json:"status"`
	FromWalletID  string    `json:"fromWalletId"`
	ToWalletID    string    `json:"toWalletId"`
	Amount        int64     `json:"amount"`
	FailureReason string    `json:"failureReason,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
}

func toTransferResponse(t *domain.Transfer) transferResponse {
	return transferResponse{
		TransferID:    t.ID,
		Status:        string(t.Status),
		FromWalletID:  t.FromWalletID,
		ToWalletID:    t.ToWalletID,
		Amount:        t.Amount,
		FailureReason: t.FailureReason,
		CreatedAt:     t.CreatedAt,
	}
}

type ledgerEntryResponse struct {
	ID        string    `json:"id"`
	WalletID  string    `json:"walletId"`
	Type      string    `json:"type"`
	Amount    int64     `json:"amount"`
	CreatedAt time.Time `json:"createdAt"`
}

type transferDetailResponse struct {
	transferResponse
	LedgerEntries []ledgerEntryResponse `json:"ledgerEntries"`
}

type walletResponse struct {
	ID        string    `json:"id"`
	Balance   int64     `json:"balance"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type createWalletRequest struct {
	ID             string `json:"id"`
	InitialBalance int64  `json:"initialBalance"`
}

type errorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}
