package dto

import (
	"encoding/json"

	"github.com/Robustrade/wallet-transfer-assignment/model"
)

// TransferView is the API-facing transfer shape.
type TransferView struct {
	TransferID     string  `json:"transferId"`
	IdempotencyKey string  `json:"idempotencyKey"`
	FromWalletID   string  `json:"fromWalletId"`
	ToWalletID     string  `json:"toWalletId"`
	Amount         int64   `json:"amount"`
	Status         string  `json:"status"`
	FailureReason  *string `json:"failureReason,omitempty"`
}

// NewTransferView maps a model transfer to the API view.
func NewTransferView(t *model.Transfer) TransferView {
	return TransferView{
		TransferID:     t.ID.String(),
		IdempotencyKey: t.IdempotencyKey,
		FromWalletID:   t.FromWalletID,
		ToWalletID:     t.ToWalletID,
		Amount:         t.Amount.Int64(),
		Status:         string(t.Status),
		FailureReason:  t.FailureReason,
	}
}

// TransferOutcome is the result of CreateTransfer: a fresh outcome or a replay.
type TransferOutcome struct {
	StatusCode int
	Transfer   *TransferView
	// ReplayBody is set on idempotent replay; handler writes it as-is.
	ReplayBody json.RawMessage
}

// IsReplay reports whether the outcome is a stored idempotent response.
func (o TransferOutcome) IsReplay() bool {
	return len(o.ReplayBody) > 0
}
