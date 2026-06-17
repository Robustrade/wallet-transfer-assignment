package transfer

import "time"

type TransferState string

const (
	Pending   TransferState = "PENDING"
	Processed TransferState = "PROCESSED"
	Failed    TransferState = "FAILED"
)

type Transfer struct {
	ID             uint64        `json:"id"`
	IdempotencyKey string        `json:"idempotencyKey"`
	FromWalletID   string        `json:"fromWalletId"`
	ToWalletID     string        `json:"toWalletId"`
	Amount         float64       `json:"amount"`
	State          TransferState `json:"state"`
	FailureReason  string        `json:"failureReason,omitempty"`
	CreatedAt      time.Time     `json:"createdAt"`
	UpdatedAt      time.Time     `json:"updatedAt"`
}
