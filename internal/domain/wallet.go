package domain

import (
	"fmt"
	"math"
	"strings"
	"time"
	"unicode/utf8"
)

const maxWalletIDLen = 64

// Wallet holds a balance in minor units (e.g. paise or cents).
type Wallet struct {
	ID             string
	Balance        int64
	OpeningBalance int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// ValidateWalletID checks that id is storable and within bounds. It runs before
// any repository access so bad input is a 400, not a database error.
func ValidateWalletID(id string) error {
	if !validText(id, maxWalletIDLen) {
		return fmt.Errorf("%w: wallet id must be 1-%d characters of valid text", ErrInvalidRequest, maxWalletIDLen)
	}
	return nil
}

// validText reports whether s is 1..maxLen bytes of valid UTF-8 without NUL or
// the replacement character U+FFFD.
//
// PostgreSQL TEXT rejects NUL and invalid UTF-8, which would otherwise surface
// as an internal error. U+FFFD is refused because JSON decoders substitute it
// for invalid bytes and lone surrogate escapes, so allowing it would let many
// different malformed inputs collapse into one identifier.
func validText(s string, maxLen int) bool {
	return s != "" && len(s) <= maxLen && utf8.ValidString(s) &&
		!strings.ContainsRune(s, 0) && !strings.ContainsRune(s, utf8.RuneError)
}

// NewWallet validates and builds a wallet opened with openingBalance.
func NewWallet(id string, openingBalance int64, now time.Time) (*Wallet, error) {
	if err := ValidateWalletID(id); err != nil {
		return nil, err
	}
	if openingBalance < 0 {
		return nil, fmt.Errorf("%w: opening balance must not be negative", ErrInvalidRequest)
	}
	return &Wallet{ID: id, Balance: openingBalance, OpeningBalance: openingBalance, CreatedAt: now, UpdatedAt: now}, nil
}

// Debit removes amount from the balance. It never lets the balance go negative.
func (w *Wallet) Debit(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: amount must be positive", ErrInvalidRequest)
	}
	if w.Balance < amount {
		return ErrInsufficientFunds
	}
	w.Balance -= amount
	return nil
}

// Credit adds amount to the balance, refusing to overflow int64.
func (w *Wallet) Credit(amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("%w: amount must be positive", ErrInvalidRequest)
	}
	if w.Balance > math.MaxInt64-amount {
		return ErrBalanceOverflow
	}
	w.Balance += amount
	return nil
}
