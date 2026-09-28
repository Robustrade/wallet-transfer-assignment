package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

// Limits from the API contract.
const (
	MaxIdempotencyKeyLen = 128
	MaxWalletIDLen       = 64
	MaxAmount            = int64(100_000_000) // minor units
)

// identifierPattern applies to idempotency keys and wallet ids: ASCII letters,
// digits and . _ : - only. Because it is ASCII-only, a length in bytes is
// also a length in characters. It also keeps '|' (and any other separator)
// out of wallet ids.
var identifierPattern = regexp.MustCompile(`^[A-Za-z0-9._:-]+$`)

// CreateTransferCommand is the input to TransferService.CreateTransfer. It is
// transport-agnostic: the HTTP handler builds it from JSON, and a test or CLI
// could build it directly.
type CreateTransferCommand struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64 // minor units
}

// ValidationError describes the first invalid field of a command. It matches
// domain.ErrValidation through errors.Is, so callers can detect "bad input"
// without knowing this type, and use Field/Message for a precise response.
//
// Field uses the JSON name because clients see it.
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string { return e.Field + ": " + e.Message }

// Unwrap makes errors.Is(err, domain.ErrValidation) true.
func (e *ValidationError) Unwrap() error { return domain.ErrValidation }

// Validate checks every rule in the API contract and returns the first
// violation, checking fields in a fixed order: idempotencyKey, fromWalletId,
// toWalletId, then amount.
//
// The HTTP handler calls this after decoding JSON, and the service calls it
// again (defense in depth), so the rules live in exactly one place.
func (c CreateTransferCommand) Validate() error {
	if err := validateIdentifier("idempotencyKey", c.IdempotencyKey, MaxIdempotencyKeyLen); err != nil {
		return err
	}
	if err := validateIdentifier("fromWalletId", c.FromWalletID, MaxWalletIDLen); err != nil {
		return err
	}
	if err := validateIdentifier("toWalletId", c.ToWalletID, MaxWalletIDLen); err != nil {
		return err
	}
	// Wallet ids are case-sensitive, so "W1" and "w1" are different wallets.
	if c.FromWalletID == c.ToWalletID {
		return &ValidationError{Field: "toWalletId", Message: "must differ from fromWalletId"}
	}
	if c.Amount < 1 {
		return &ValidationError{Field: "amount", Message: "must be at least 1"}
	}
	if c.Amount > MaxAmount {
		return &ValidationError{Field: "amount", Message: fmt.Sprintf("must be at most %d", MaxAmount)}
	}
	return nil
}

func validateIdentifier(field, value string, maxLen int) error {
	switch {
	case value == "":
		return &ValidationError{Field: field, Message: "is required"}
	case len(value) > maxLen:
		return &ValidationError{Field: field, Message: fmt.Sprintf("must be at most %d characters", maxLen)}
	case !identifierPattern.MatchString(value):
		return &ValidationError{Field: field, Message: "may only contain letters, digits and . _ : -"}
	}
	return nil
}

// RequestHash is a fingerprint of the payload, stored with the idempotency
// key. A retry with the same key must carry the same hash. Otherwise the key
// is being reused for a different request, and the service rejects it (409).
//
// The hash covers the canonical fields rather than the raw JSON, so key order
// and whitespace do not matter. The idempotency key itself is left out: it is
// the lookup key, not part of the payload.
//
// Each wallet id is length-prefixed ("8:wallet_1") so field boundaries are
// unambiguous. With a plain "from|to|amount" encoding, from="a|b",to="c" and
// from="a",to="b|c" would collide.
//
// The encoding is persisted, so changing it would make every stored hash
// mismatch. A golden-vector test guards against that.
func (c CreateTransferCommand) RequestHash() string {
	canonical := fmt.Sprintf("%d:%s|%d:%s|%d",
		len(c.FromWalletID), c.FromWalletID,
		len(c.ToWalletID), c.ToWalletID,
		c.Amount)
	sum := sha256.Sum256([]byte(canonical))
	return hex.EncodeToString(sum[:])
}
