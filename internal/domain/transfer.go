package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TransferStatus is the transfer state machine. The only legal transitions
// are PENDING -> PROCESSED and PENDING -> FAILED; both are terminal. The
// database enforces the same rule independently via a trigger (see
// docs/design.md §3.3), so the invariant holds even if this code has a bug.
type TransferStatus string

const (
	TransferStatusPending   TransferStatus = "PENDING"
	TransferStatusProcessed TransferStatus = "PROCESSED"
	TransferStatusFailed    TransferStatus = "FAILED"
)

// CanTransitionTo reports whether moving from s to next is a legal state
// transition.
func (s TransferStatus) CanTransitionTo(next TransferStatus) bool {
	if s != TransferStatusPending {
		return false
	}
	return next == TransferStatusProcessed || next == TransferStatusFailed
}

// FailureCode enumerates the reasons a transfer can land in FAILED.
type FailureCode string

const FailureCodeInsufficientFunds FailureCode = "INSUFFICIENT_FUNDS"

// Transfer is the domain representation of a transfer row.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	RequestHash    string
	FromWalletID   string
	ToWalletID     string
	AmountMinor    int64
	Currency       string
	Status         TransferStatus
	FailureCode    FailureCode
	FailureMessage string
	CreatedAt      time.Time
	UpdatedAt      time.Time
	SettledAt      *time.Time
}

// TransferCommand is the validated input to create a transfer.
type TransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	AmountMinor    int64
	Currency       string // empty means "inherit from the wallets"
}

const maxIdempotencyKeyLen = 255

// Validate checks the command's shape in isolation, without touching any
// storage. It does not check that the wallets exist or share a currency —
// those checks require a lookup and happen in the service layer.
func (c TransferCommand) Validate() error {
	if c.IdempotencyKey == "" {
		return NewValidationError("idempotencyKey", "idempotencyKey is required")
	}
	if len(c.IdempotencyKey) > maxIdempotencyKeyLen {
		return NewValidationError("idempotencyKey", fmt.Sprintf("idempotencyKey must be at most %d characters", maxIdempotencyKeyLen))
	}
	if !isPrintableASCII(c.IdempotencyKey) {
		return NewValidationError("idempotencyKey", "idempotencyKey must be printable ASCII")
	}
	if !ValidWalletID(c.FromWalletID) {
		return NewValidationError("fromWalletId", "fromWalletId is not a valid wallet id")
	}
	if !ValidWalletID(c.ToWalletID) {
		return NewValidationError("toWalletId", "toWalletId is not a valid wallet id")
	}
	if c.FromWalletID == c.ToWalletID {
		return NewValidationError("toWalletId", "fromWalletId and toWalletId must differ")
	}
	if !ValidAmount(c.AmountMinor) {
		return NewValidationError("amount", "amount must be greater than zero and within the allowed maximum")
	}
	if c.Currency != "" && !ValidCurrency(c.Currency) {
		return NewValidationError("currency", "currency must be a 3-letter ISO 4217 code")
	}
	return nil
}

func isPrintableASCII(s string) bool {
	for _, r := range s {
		if r < 0x20 || r > 0x7e {
			return false
		}
	}
	return true
}

// Fingerprint returns a stable hash of the fields that must match for a
// replayed idempotency key to be considered the same logical request. It is
// versioned ("v1|...") so the hashing scheme can change later without
// colliding with fingerprints already stored.
func (c TransferCommand) Fingerprint() string {
	h := sha256.New()
	fmt.Fprintf(h, "v1|%s|%s|%d|%s", c.FromWalletID, c.ToWalletID, c.AmountMinor, c.Currency)
	return hex.EncodeToString(h.Sum(nil))
}

// NewPendingTransfer builds a new PENDING transfer from a validated command.
func NewPendingTransfer(id uuid.UUID, cmd TransferCommand, currency string) Transfer {
	return Transfer{
		ID:             id,
		IdempotencyKey: cmd.IdempotencyKey,
		RequestHash:    cmd.Fingerprint(),
		FromWalletID:   cmd.FromWalletID,
		ToWalletID:     cmd.ToWalletID,
		AmountMinor:    cmd.AmountMinor,
		Currency:       currency,
		Status:         TransferStatusPending,
	}
}
