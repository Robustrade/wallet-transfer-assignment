package domain

import "time"

// Transfer status constants define the allowed states.
const (
	TransferStatusPending   = "PENDING"
	TransferStatusProcessed = "PROCESSED"
	TransferStatusFailed    = "FAILED"
)

// Transfer represents a wallet-to-wallet money transfer.
type Transfer struct {
	ID             string    `json:"id"`
	IdempotencyKey string    `json:"idempotencyKey"`
	FromWalletID   string    `json:"fromWalletId"`
	ToWalletID     string    `json:"toWalletId"`
	Amount         int64     `json:"amount"`
	Status         string    `json:"status"`
	FailureReason  *string   `json:"failureReason,omitempty"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

// TransferRequest is the inbound API request for creating a transfer.
type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// Validate checks that the transfer request contains valid data.
func (r TransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return ErrMissingIdempotencyKey
	}
	if r.FromWalletID == "" {
		return ErrMissingFromWallet
	}
	if r.ToWalletID == "" {
		return ErrMissingToWallet
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	// Max amount set to 1 trillion to prevent integer overflow in DB/Go
	if r.Amount > 1_000_000_000_000 {
		return ErrInvalidAmount
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSelfTransfer
	}
	return nil
}

// IsTerminal returns true if the transfer is in a final state.
func (t Transfer) IsTerminal() bool {
	return t.Status == TransferStatusProcessed || t.Status == TransferStatusFailed
}
