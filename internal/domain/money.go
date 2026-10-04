package domain

import "regexp"

// MaxAmount is the largest accepted monetary amount, in minor units.
// It sits well below the int64 limit so a credit of a legal amount cannot
// overflow a balance that is itself within range by accident of addition order
// without an explicit overflow check.
const MaxAmount int64 = 1_000_000_000_000_000

var (
	walletIDPattern       = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	idempotencyKeyPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`)
)

// ValidateWalletID checks the client-supplied wallet identifier.
func ValidateWalletID(id string) error {
	if !walletIDPattern.MatchString(id) {
		return &ValidationError{Details: []FieldError{{
			Field:   "id",
			Message: "must be 1-64 characters and contain only letters, digits, '_' or '-'",
		}}}
	}
	return nil
}

// ValidateIdempotencyKey checks the client-supplied idempotency key.
func ValidateIdempotencyKey(key string) error {
	if !idempotencyKeyPattern.MatchString(key) {
		return &ValidationError{Details: []FieldError{{
			Field:   "idempotencyKey",
			Message: "must be 1-128 characters and contain only letters, digits, '_' or '-'",
		}}}
	}
	return nil
}

// ValidateTransferAmount checks a transfer amount.
func ValidateTransferAmount(amount int64) error {
	if amount <= 0 {
		return &ValidationError{Details: []FieldError{{
			Field:   "amount",
			Message: "must be greater than 0",
		}}}
	}
	if amount > MaxAmount {
		return &ValidationError{Details: []FieldError{{
			Field:   "amount",
			Message: "exceeds the maximum allowed amount",
		}}}
	}
	return nil
}

// ValidateInitialBalance checks opening funds for a new wallet.
func ValidateInitialBalance(amount int64) error {
	if amount < 0 {
		return &ValidationError{Details: []FieldError{{
			Field:   "initialBalance",
			Message: "must be greater than or equal to 0",
		}}}
	}
	if amount > MaxAmount {
		return &ValidationError{Details: []FieldError{{
			Field:   "initialBalance",
			Message: "exceeds the maximum allowed amount",
		}}}
	}
	return nil
}
