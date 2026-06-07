package domain

// TransferRequest is a validated description of a requested transfer. It is the
// input the service layer works with, decoupled from any transport (HTTP) type.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Validate enforces the business invariants that must hold for every transfer
// regardless of wallet state. Stateful checks (existence, balance, currency
// match) are performed by the service inside the transaction.
func (r TransferRequest) Validate() error {
	if r.IdempotencyKey == "" {
		return newValidationError("idempotencyKey", "idempotency key is required")
	}
	if r.FromWalletID == "" {
		return newValidationError("fromWalletId", "source wallet id is required")
	}
	if r.ToWalletID == "" {
		return newValidationError("toWalletId", "destination wallet id is required")
	}
	if r.FromWalletID == r.ToWalletID {
		return newValidationError("toWalletId", "source and destination wallets must differ")
	}
	if r.Amount <= 0 {
		return newValidationError("amount", "amount must be a positive integer")
	}
	return nil
}
