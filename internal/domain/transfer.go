package domain

import "strings"

type TransferStatus string

const (
	TransferPending   TransferStatus = "PENDING"
	TransferProcessed TransferStatus = "PROCESSED"
	TransferFailed    TransferStatus = "FAILED"
)

// Transfer represents a single movement of funds between two different wallets.
type Transfer struct {
	ID           string
	FromWalletID string
	ToWalletID   string
	Amount       Amount
	Status       TransferStatus
	FailureCode  string
}

func NewTransfer(id, fromWalletID, toWalletID string, amount Amount) (Transfer, error) {
	transfer := Transfer{
		ID:           id,
		FromWalletID: fromWalletID,
		ToWalletID:   toWalletID,
		Amount:       amount,
		Status:       TransferPending,
	}
	if err := transfer.Validate(); err != nil {
		return Transfer{}, err
	}
	return transfer, nil
}

func (t Transfer) Validate() error {
	if t.ID == "" {
		return ErrInvalidTransferID
	}
	if t.FromWalletID == "" || t.ToWalletID == "" {
		return ErrInvalidWalletID
	}
	if t.FromWalletID == t.ToWalletID {
		return ErrSameWallet
	}
	if t.Amount <= 0 {
		return ErrInvalidAmount
	}
	switch t.Status {
	case TransferPending, TransferProcessed:
		if t.FailureCode != "" {
			return ErrInvalidTransferStatus
		}
	case TransferFailed:
		if strings.TrimSpace(t.FailureCode) == "" {
			return ErrInvalidFailureCode
		}
	default:
		return ErrInvalidTransferStatus
	}
	return nil
}

// MarkProcessed permits only the PENDING -> PROCESSED transition.
func (t *Transfer) MarkProcessed() error {
	if t.Status != TransferPending {
		return ErrInvalidTransferTransition
	}
	t.Status = TransferProcessed
	t.FailureCode = ""
	return nil
}

// MarkFailed permits only the PENDING -> FAILED transition.
func (t *Transfer) MarkFailed(failureCode string) error {
	if t.Status != TransferPending {
		return ErrInvalidTransferTransition
	}
	if strings.TrimSpace(failureCode) == "" {
		return ErrInvalidFailureCode
	}
	t.Status = TransferFailed
	t.FailureCode = failureCode
	return nil
}
