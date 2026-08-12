package domain

import (
	"regexp"
	"time"
)

// WalletKind distinguishes ordinary user wallets from system wallets, which
// are permitted to hold a negative balance because they model the boundary
// between this ledger and the outside world.
type WalletKind string

const (
	WalletKindUser   WalletKind = "USER"
	WalletKindSystem WalletKind = "SYSTEM"
)

var walletIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// ValidWalletID reports whether id is a legal wallet identifier.
func ValidWalletID(id string) bool {
	return walletIDPattern.MatchString(id)
}

// Wallet is the domain representation of a wallet row. BalanceMinor is the
// stored, lockable projection of the ledger described in docs/design.md §3.4.
type Wallet struct {
	ID           string
	Kind         WalletKind
	Currency     string
	BalanceMinor int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// CanDebit reports whether amountMinor may be subtracted from the wallet's
// current balance without violating the non-negative-balance rule for user
// wallets. System wallets may always be debited.
func (w Wallet) CanDebit(amountMinor int64) bool {
	if w.Kind == WalletKindSystem {
		return true
	}
	return w.BalanceMinor >= amountMinor
}

// NewWallet constructs a zero-balance user wallet. System wallets are only
// ever created by migration seed data, not through this constructor.
func NewWallet(id, currency string) (Wallet, error) {
	if !ValidWalletID(id) {
		return Wallet{}, NewValidationError("id", "wallet id must be 1-64 chars of letters, digits, underscore or hyphen")
	}
	if !ValidCurrency(currency) {
		return Wallet{}, NewValidationError("currency", "currency must be a 3-letter ISO 4217 code")
	}
	return Wallet{
		ID:       id,
		Kind:     WalletKindUser,
		Currency: currency,
	}, nil
}
