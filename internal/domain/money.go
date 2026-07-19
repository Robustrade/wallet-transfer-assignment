package domain

import (
	"strings"
	"unicode"
)

const DefaultCurrency = "USD"

type Money struct {
	Amount   int64
	Currency string
}

func NewMoney(amount int64, currency string) (Money, error) {
	currency = NormalizeCurrency(currency)
	if amount <= 0 {
		return Money{}, ErrInvalidAmount
	}
	if !IsValidCurrency(currency) {
		return Money{}, ErrInvalidCurrency
	}
	return Money{Amount: amount, Currency: currency}, nil
}

func NormalizeCurrency(currency string) string {
	currency = strings.TrimSpace(currency)
	if currency == "" {
		return DefaultCurrency
	}
	return strings.ToUpper(currency)
}

func IsValidCurrency(currency string) bool {
	if len(currency) != 3 {
		return false
	}
	for _, char := range currency {
		if !unicode.IsUpper(char) || !unicode.IsLetter(char) {
			return false
		}
	}
	return true
}
