package domain

import "errors"

var (
	ErrWalletNotFound         = errors.New("wallet not found")
	ErrInsufficientBalance    = errors.New("insufficient balance")
	ErrSameWallet             = errors.New("from and to wallet cannot be the same")
	ErrIdempotencyKeyConflict = errors.New("idempotency key already used with different parameters")
	ErrSomethingWentWrong     = errors.New("something went wrong")
	ErrTransferFailed         = errors.New("transfer failed")
)
