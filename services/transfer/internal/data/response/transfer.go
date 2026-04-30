// Package response holds outbound DTOs for the transfer service.
package response

import (
	"time"

	"github.com/google/uuid"

	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
)

// TransferResp is the JSON body returned by POST /transfers.
//
// `Replayed` indicates whether this response was returned for a duplicate
// idempotency key (true) or a freshly-executed transfer (false). Handlers
// use this to choose between 201 Created (fresh) and 200 OK (replay).
type TransferResp struct {
	ID             uuid.UUID `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	FailureReason  *string   `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
	Replayed       bool      `json:"replayed"`
}

// IsFailed reports whether the transfer terminated in FAILED state. Handlers
// use this to choose 422 Unprocessable for fresh failures.
func (r TransferResp) IsFailed() bool {
	return r.Status == string(model.StatusFailed)
}

// FromTransfer maps a domain Transfer to the wire response.
func FromTransfer(t model.Transfer, replayed bool) TransferResp {
	return TransferResp{
		ID:             t.ID,
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount.Minor(),
		Currency:       t.Currency,
		Status:         string(t.Status),
		FailureReason:  t.FailureReason,
		CreatedAt:      t.CreatedAt,
		UpdatedAt:      t.UpdatedAt,
		Replayed:       replayed,
	}
}
