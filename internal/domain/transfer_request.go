package domain

import "fmt"

// TransferRequest is the validated input for creating a transfer.
type TransferRequest struct {
	IdempotencyKey string `json:"idempotencyKey"`
	FromWalletID   string `json:"fromWalletId"`
	ToWalletID     string `json:"toWalletId"`
	Amount         int64  `json:"amount"`
}

// Validate checks request shape before any repository/service work begins.
func (r TransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotencyKey", ErrMissingField)
	}
	if r.FromWalletID == "" {
		return fmt.Errorf("%w: fromWalletId", ErrMissingField)
	}
	if r.ToWalletID == "" {
		return fmt.Errorf("%w: toWalletId", ErrMissingField)
	}
	if r.FromWalletID == r.ToWalletID {
		return ErrSameWallet
	}
	if r.Amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}

// Fingerprint identifies the logical content of the request, independent
// of the idempotency key itself, so a replayed key can be checked against
// the original payload.
func (r TransferRequest) Fingerprint() string {
	return fmt.Sprintf("%s|%s|%d", r.FromWalletID, r.ToWalletID, r.Amount)
}
