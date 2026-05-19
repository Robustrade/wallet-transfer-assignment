// apperr holds the typed errors that cross layer boundaries.
package apperr

import "errors"

var (
	ErrWalletNotFound    = errors.New("wallet not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrKeyReused         = errors.New("idempotency key reused with different parameters")
	ErrInProgress        = errors.New("request in progress")
	ErrInvalidRequest    = errors.New("invalid request")
	ErrSameWallet        = errors.New("from and to wallet must be different")
)
