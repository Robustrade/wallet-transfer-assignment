// Package domain holds the wallet transfer entities and their rules. It does no I/O.
package domain

import (
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

type TransferStatus string

const (
	StatusPending   TransferStatus = "PENDING"
	StatusProcessed TransferStatus = "PROCESSED"
	StatusFailed    TransferStatus = "FAILED"
)

type FailureReason string

const FailureInsufficientFunds FailureReason = "INSUFFICIENT_FUNDS"

// MaxIdempotencyKeyLength matches the CHECK constraint on transfers.idempotency_key.
const MaxIdempotencyKeyLength = 255

// TransferRequest is a validated-on-demand request to move Amount (minor units)
// between two wallets. An empty IdempotencyKey means the request is not deduplicated.
type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// Validate checks the request's own fields. Whether the wallets exist and whether
// the source can afford the amount are decided later, inside the transaction.
func (r TransferRequest) Validate() error {
	switch {
	case utf8.RuneCountInString(r.IdempotencyKey) > MaxIdempotencyKeyLength:
		return &ValidationError{Field: "idempotencyKey", Message: fmt.Sprintf("must be at most %d characters", MaxIdempotencyKeyLength)}
	case r.FromWalletID == "":
		return &ValidationError{Field: "fromWalletId", Message: "is required"}
	case r.ToWalletID == "":
		return &ValidationError{Field: "toWalletId", Message: "is required"}
	case r.FromWalletID == r.ToWalletID:
		return &ValidationError{Field: "toWalletId", Message: "must differ from fromWalletId"}
	case r.Amount <= 0:
		return &ValidationError{Field: "amount", Message: "must be a positive integer in minor units"}
	}
	return nil
}

// Transfer is the record of one transfer attempt and its outcome.
type Transfer struct {
	ID             uuid.UUID
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         TransferStatus
	FailureReason  FailureReason
	CreatedAt      time.Time
}

// NewTransfer creates a PENDING transfer for a request.
func NewTransfer(id uuid.UUID, req TransferRequest) Transfer {
	return Transfer{
		ID:             id,
		IdempotencyKey: req.IdempotencyKey,
		FromWalletID:   req.FromWalletID,
		ToWalletID:     req.ToWalletID,
		Amount:         req.Amount,
		Status:         StatusPending,
	}
}

// MarkProcessed moves a PENDING transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	if t.Status != StatusPending {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, StatusProcessed)
	}
	t.Status = StatusProcessed
	return nil
}

// MarkFailed moves a PENDING transfer to FAILED with a reason.
func (t *Transfer) MarkFailed(reason FailureReason) error {
	if t.Status != StatusPending {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, t.Status, StatusFailed)
	}
	if reason == "" {
		return errors.New("failure reason is required")
	}
	t.Status = StatusFailed
	t.FailureReason = reason
	return nil
}

// MatchesRequest reports whether req carries the same payload as the request that
// created this transfer. Used to tell a retry from a reused idempotency key.
func (t Transfer) MatchesRequest(req TransferRequest) bool {
	return t.FromWalletID == req.FromWalletID &&
		t.ToWalletID == req.ToWalletID &&
		t.Amount == req.Amount
}

// LedgerEntries returns the double-entry pair for a processed transfer:
// a DEBIT on the source and a CREDIT on the destination, both for the full amount.
// Pending and failed transfers move no money and have no entries.
func (t Transfer) LedgerEntries() []LedgerEntry {
	if t.Status != StatusProcessed {
		return nil
	}
	return []LedgerEntry{
		{TransferID: t.ID, WalletID: t.FromWalletID, Type: EntryDebit, Amount: t.Amount},
		{TransferID: t.ID, WalletID: t.ToWalletID, Type: EntryCredit, Amount: t.Amount},
	}
}
