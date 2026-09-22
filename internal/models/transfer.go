// Package models defines the request/response payload structures for the
// HTTP transport layer, decoupled from both domain entities and the
// service layer's own input/output types.
package models

import (
	"time"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/service"
)

// CreateTransferRequest is the wire format for POST /transfers.
type CreateTransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// ToInput converts the wire payload into the service layer's input type.
func (r CreateTransferRequest) ToInput() service.CreateTransferInput {
	return service.CreateTransferInput{
		IdempotencyKey: r.IdempotencyKey,
		FromWalletID:   r.FromWalletID,
		ToWalletID:     r.ToWalletID,
		Amount:         r.Amount,
	}
}

// LedgerEntryResponse is the wire format for a single ledger entry.
type LedgerEntryResponse struct {
	ID         string    `json:"id"`
	WalletID   string    `json:"walletId"`
	TransferID string    `json:"transferId"`
	Type       string    `json:"type"`
	Amount     int64     `json:"amount"`
	CreatedAt  time.Time `json:"createdAt"`
}

// TransferResponse is the wire format for a transfer, optionally including
// its ledger entries.
type TransferResponse struct {
	ID             string                `json:"id"`
	IdempotencyKey string                `json:"idempotencyKey"`
	FromWalletID   string                `json:"fromWalletId"`
	ToWalletID     string                `json:"toWalletId"`
	Amount         int64                 `json:"amount"`
	State          string                `json:"state"`
	FailureReason  string                `json:"failureReason,omitempty"`
	CreatedAt      time.Time             `json:"createdAt"`
	UpdatedAt      time.Time             `json:"updatedAt"`
	LedgerEntries  []LedgerEntryResponse `json:"ledgerEntries,omitempty"`
}

// ToTransferResponse builds a TransferResponse from domain types.
func ToTransferResponse(t domain.Transfer, entries []domain.LedgerEntry) TransferResponse {
	resp := TransferResponse{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount,
		State:          string(t.State),
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
	}
	for _, e := range entries {
		resp.LedgerEntries = append(resp.LedgerEntries, LedgerEntryResponse{
			ID:         e.ID,
			WalletID:   e.WalletID,
			TransferID: e.TransferID,
			Type:       string(e.Type),
			Amount:     e.Amount,
			CreatedAt:  e.CreatedAt,
		})
	}
	return resp
}
