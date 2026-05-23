package wallet

import "errors"

var (
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrWalletInactive    = errors.New("wallet is inactive")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrSameWallet        = errors.New("cannot transfer to the same wallet")
)
