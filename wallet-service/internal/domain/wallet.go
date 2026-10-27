package domain

import "math"

type Wallet struct {
	ID       string
	Currency string
	Balance  int64 // Minor currency units.
}

func ValidateWalletPair(from, to Wallet) error {
	if from.Currency != to.Currency {
		return ErrCurrencyMismatch
	}
	switch from.Currency {
	case "INR", "USD", "JPY", "KWD":
		return nil
	default:
		return ErrUnsupportedCurrency
	}
}

func CheckTransferBalances(from, to Wallet, amount int64) error {
	if amount <= 0 || from.Balance < 0 || to.Balance < 0 {
		return ErrInvalidRequest
	}
	if from.Balance < amount {
		return ErrInsufficientFunds
	}
	if to.Balance > math.MaxInt64-amount {
		return ErrBalanceOverflow
	}
	return nil
}
