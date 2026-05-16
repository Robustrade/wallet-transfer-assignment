package domain

import (
	"errors"
)

var (
	ErrInvalidAmount     = errors.New("amount must be positive")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrInvalidWallet     = errors.New("invalid wallet")
)
