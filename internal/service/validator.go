package service

import (
	"errors"
	"fmt"

	"wallet-transfer-assignment/internal/domain"
)

var ErrInvalidRequest = errors.New("invalid request")

func validateCreateTransferRequest(req domain.CreateTransferRequest) error {
	if req.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency key is required", ErrInvalidRequest)
	}

	if req.FromWalletId == "" || req.ToWalletId == "" {
		return fmt.Errorf("%w: wallet id is required", ErrInvalidRequest)
	}

	if req.Amount <= 0 {
		return fmt.Errorf("%w: amount must be greater than zero", ErrInvalidRequest)
	}

	if req.FromWalletId == req.ToWalletId {
		return fmt.Errorf("%w: source and destination wallets must be different", ErrInvalidRequest)
	}

	return nil
}
