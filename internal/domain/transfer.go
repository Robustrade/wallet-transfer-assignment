package domain

import "time"

// TransferState is the lifecycle state of a transfer.
type TransferState string

const (
	TransferStatePending   TransferState = "PENDING"
	TransferStateProcessed TransferState = "PROCESSED"
	TransferStateFailed    TransferState = "FAILED"
)

// Transfer represents a wallet-to-wallet transfer.
type Transfer struct {
	ID             string
	SourceWalletID string
	TargetWalletID string
	Amount         int64
	Currency       string
	State          TransferState
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// CanTransitionTo reports whether moving to next is allowed.
func (s TransferState) CanTransitionTo(next TransferState) bool {
	switch s {
	case TransferStatePending:
		return next == TransferStateProcessed || next == TransferStateFailed
	default:
		return false
	}
}

// ValidateTransferRequest checks domain rules before persisting a transfer.
// TODO: validate amount, wallets, currency match, etc.
func ValidateTransferRequest(sourceWalletID, targetWalletID string, amount int64) error {
	if sourceWalletID == targetWalletID {
		return ErrSameWallet
	}
	if amount <= 0 {
		return ErrInvalidTransfer
	}
	return nil
}
