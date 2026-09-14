package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// MaxIdempotencyKeyLength bounds a key so a caller cannot store unbounded text
// in an indexed column.
//
// Measured in bytes, not characters, because the limit is about storage: 255
// CJK characters occupy 765 bytes, and counting runes would let them past a
// limit whose purpose is to cap what gets stored. Validation uses len() for the
// same reason, and the error message says bytes.
const MaxIdempotencyKeyLength = 255

// FailureReasonInsufficientFunds is recorded on a transfer whose source wallet
// could not cover the amount. It is stored data rather than a log message: a
// replay of the same idempotency key reports it back verbatim.
const FailureReasonInsufficientFunds = "insufficient funds"

// State is the lifecycle of a transfer. A transfer is created PENDING and moves
// exactly once, to PROCESSED or FAILED, within a single database transaction.
type State string

const (
	// StatePending means the idempotency key is claimed but no money has moved.
	// It is never externally observable: the transition out of it commits in the
	// same transaction that created it.
	StatePending State = "PENDING"

	// StateProcessed means both balances and both ledger entries are written.
	StateProcessed State = "PROCESSED"

	// StateFailed means the transfer will never succeed. It is committed rather
	// than rolled back, so a replay of the idempotency key reports the same
	// failure instead of re-attempting the debit.
	StateFailed State = "FAILED"
)

// IsTerminal reports whether a transfer in this state has a final answer.
func (s State) IsTerminal() bool {
	return s == StateProcessed || s == StateFailed
}

// Transfer is a single movement of money between two wallets.
type Transfer struct {
	ID             string
	IdempotencyKey string
	RequestHash    string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	State          State
	FailureReason  string
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// MarkProcessed moves a pending transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	if t.State != StatePending {
		return ErrInvalidStateTransition
	}
	t.State = StateProcessed
	t.FailureReason = ""
	return nil
}

// MarkFailed moves a pending transfer to FAILED with the reason it failed. The
// reason is required: the schema enforces that a transfer is FAILED if and only
// if it carries one.
func (t *Transfer) MarkFailed(reason string) error {
	if t.State != StatePending {
		return ErrInvalidStateTransition
	}
	if strings.TrimSpace(reason) == "" {
		return ErrInvalidStateTransition
	}
	t.State = StateFailed
	t.FailureReason = reason
	return nil
}

// TransferRequest is a validated intent to move money. It is the input to the
// service, already parsed out of whatever transport delivered it.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Validate reports the first rule the request breaks.
func (r TransferRequest) Validate() error {
	switch {
	case r.IdempotencyKey == "":
		return ErrMissingIdempotencyKey
	case len(r.IdempotencyKey) > MaxIdempotencyKeyLength:
		return ErrIdempotencyKeyTooLong
	case r.FromWalletID == "":
		return ErrMissingFromWallet
	case r.ToWalletID == "":
		return ErrMissingToWallet
	case r.FromWalletID == r.ToWalletID:
		return ErrSameWallet
	case r.Amount <= 0:
		return ErrNonPositiveAmount
	}
	return nil
}

// Hash fingerprints the parameters this request carries, so an idempotency key
// replayed with different parameters can be told apart from a genuine retry.
//
// It hashes the parsed fields rather than the raw request body: two bodies
// differing only in whitespace, field order, or an ignored field describe the
// same transfer and must not be reported as a conflict. The idempotency key is
// excluded because it is the lookup key, not part of what is fingerprinted.
func (r TransferRequest) Hash() string {
	// Wallet ids are opaque and may contain any character, so fields are joined
	// with a newline and length-prefixed to keep the encoding unambiguous: no
	// two distinct requests can produce the same input string.
	var b strings.Builder
	writeField(&b, r.FromWalletID)
	writeField(&b, r.ToWalletID)
	writeField(&b, strconv.FormatInt(r.Amount, 10))

	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

func writeField(b *strings.Builder, value string) {
	b.WriteString(strconv.Itoa(len(value)))
	b.WriteByte(':')
	b.WriteString(value)
	b.WriteByte('\n')
}
