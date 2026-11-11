package domain

import "wallet-transfer-assignment/internal/appErrors"

type TransferStatus string

const (
	Pending   TransferStatus = "PENDING"
	Processed TransferStatus = "PROCESSED"
	Failed    TransferStatus = "FAILED"
)

const ReasonInsufficientFunds = "INSUFFICIENT_FUNDS"

type Transfer struct {
	ID            string
	FromWalletID  string
	ToWalletID    string
	Amount        int64
	Status        TransferStatus
	FailureReason string
}

// Transition changes state only after validating the allowed pending-to-terminal edge.
func (t *Transfer) Transition(next TransferStatus) error {
	if t.Status != Pending || (next != Processed && next != Failed) {
		return appErrors.ErrInvalidTransition
	}
	t.Status = next
	return nil
}
