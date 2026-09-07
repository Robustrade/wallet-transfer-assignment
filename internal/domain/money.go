package domain

import "fmt"

// Money is a value object representing an amount in minor units (e.g. cents)
// of a single currency. Integer math only — no floating point rounding.
type Money struct {
	// Amount is in minor units and is always >= 0 for a valid Money value.
	Amount   int64
	Currency string
}

// NewMoney constructs a Money value, rejecting negative amounts and empty
// currencies.
func NewMoney(amount int64, currency string) (Money, error) {
	if currency == "" {
		return Money{}, fmt.Errorf("%w: currency is required", ErrValidation)
	}
	if amount < 0 {
		return Money{}, fmt.Errorf("%w: amount must not be negative", ErrValidation)
	}
	return Money{Amount: amount, Currency: currency}, nil
}

// IsZero reports whether the amount is zero.
func (m Money) IsZero() bool { return m.Amount == 0 }

// IsPositive reports whether the amount is strictly greater than zero.
func (m Money) IsPositive() bool { return m.Amount > 0 }

// sameCurrency guards binary operations.
func (m Money) sameCurrency(other Money) error {
	if m.Currency != other.Currency {
		return fmt.Errorf("%w: %s vs %s", ErrCurrencyMismatch, m.Currency, other.Currency)
	}
	return nil
}

// Add returns m + other. Currencies must match.
func (m Money) Add(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	return Money{Amount: m.Amount + other.Amount, Currency: m.Currency}, nil
}

// Sub returns m - other. Currencies must match and the result must not go
// negative (that is a domain rule, not an arithmetic one).
func (m Money) Sub(other Money) (Money, error) {
	if err := m.sameCurrency(other); err != nil {
		return Money{}, err
	}
	if other.Amount > m.Amount {
		return Money{}, ErrInsufficientFunds
	}
	return Money{Amount: m.Amount - other.Amount, Currency: m.Currency}, nil
}

// GreaterThanOrEqual reports whether m >= other. Currencies must match.
func (m Money) GreaterThanOrEqual(other Money) (bool, error) {
	if err := m.sameCurrency(other); err != nil {
		return false, err
	}
	return m.Amount >= other.Amount, nil
}

// String renders the amount for logs, e.g. "100 USD".
func (m Money) String() string { return fmt.Sprintf("%d %s", m.Amount, m.Currency) }
