package domain

import "strings"

func NormalizeCreateTransferInput(input CreateTransferInput) CreateTransferInput {
	return CreateTransferInput{
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		FromWalletID:   strings.TrimSpace(input.FromWalletID),
		ToWalletID:     strings.TrimSpace(input.ToWalletID),
		Amount:         input.Amount,
	}
}

func ValidateCreateTransferInput(input CreateTransferInput) error {
	switch {
	case input.IdempotencyKey == "":
		return NewAppError(ErrorInvalidRequest, "idempotencyKey is required")
	case input.FromWalletID == "":
		return NewAppError(ErrorInvalidRequest, "fromWalletId is required")
	case input.ToWalletID == "":
		return NewAppError(ErrorInvalidRequest, "toWalletId is required")
	case input.FromWalletID == input.ToWalletID:
		return NewAppError(ErrorInvalidRequest, "fromWalletId and toWalletId must be different")
	case input.Amount <= 0:
		return NewAppError(ErrorInvalidRequest, "amount must be greater than zero")
	default:
		return nil
	}
}

func NormalizeCreateWalletInput(input CreateWalletInput) CreateWalletInput {
	return CreateWalletInput{
		ID:             strings.TrimSpace(input.ID),
		InitialBalance: input.InitialBalance,
	}
}

func ValidateCreateWalletInput(input CreateWalletInput) error {
	switch {
	case input.ID == "":
		return NewAppError(ErrorInvalidRequest, "id is required")
	case input.InitialBalance < 0:
		return NewAppError(ErrorInvalidRequest, "initialBalance cannot be negative")
	default:
		return nil
	}
}
