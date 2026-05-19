// domain holds the core types and rules.
package domain

import "errors"

// ErrInvalidAmount is returned when a transfer amount is not positive.
var ErrInvalidAmount = errors.New("amount must be greater than zero")

// ValidateAmount makes sure we never move zero or negative money.
func ValidateAmount(amount int64) error {
	if amount <= 0 {
		return ErrInvalidAmount
	}
	return nil
}
