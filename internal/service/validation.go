package service

import "github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"

// Validate checks the request invariants that can be determined before any
// database work begins.
func (in CreateTransferInput) Validate() error {
	switch {
	case in.IdempotencyKey == "":
		return domain.ErrIdempotencyKeyRequired
	case in.FromWalletID == in.ToWalletID:
		return domain.ErrSameWallet
	case in.Amount <= 0:
		return domain.ErrInvalidAmount
	default:
		return nil
	}
}
