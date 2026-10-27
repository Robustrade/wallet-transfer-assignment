package domain

// Error codes are independent of HTTP; the handler maps them to responses.
type Error string

func (e Error) Error() string { return string(e) }

const (
	ErrInvalidRequest      Error = "INVALID_REQUEST"
	ErrSelfTransfer        Error = "SELF_TRANSFER"
	ErrWalletNotFound      Error = "WALLET_NOT_FOUND"
	ErrCurrencyMismatch    Error = "CURRENCY_MISMATCH"
	ErrUnsupportedCurrency Error = "UNSUPPORTED_CURRENCY"
	ErrInsufficientFunds   Error = "INSUFFICIENT_FUNDS"
	ErrBalanceOverflow     Error = "BALANCE_OVERFLOW"
	ErrInvalidTransition   Error = "INVALID_TRANSITION"
	ErrIdempotencyConflict Error = "IDEMPOTENCY_CONFLICT"
)
