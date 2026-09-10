package helper

import "github.com/sushmitaw/wallet-transfer-assignment/internal/domain"

func ValidateTransferRequest(req domain.CreateTransferRequest) error {
	if req.IdempotencyKey == "" {
		return domain.ErrInvalidIdempotencyKey
	}

	if req.Amount <= 0 {
		return domain.ErrInvalidAmount
	}

	if req.FromWalletID == req.ToWalletID {
		return domain.ErrSameWallet
	}

	return nil
}
