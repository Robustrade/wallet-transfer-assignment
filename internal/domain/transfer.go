// Package domain holds the wallet transfer entities and their rules. It does no I/O.
package domain

import (
	"time"

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
	return nil // TODO: implement
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
	return Transfer{} // TODO: implement
}

// MarkProcessed moves a PENDING transfer to PROCESSED.
func (t *Transfer) MarkProcessed() error {
	return nil // TODO: implement
}

// MarkFailed moves a PENDING transfer to FAILED with a reason.
func (t *Transfer) MarkFailed(reason FailureReason) error {
	return nil // TODO: implement
}

// MatchesRequest reports whether req carries the same payload as the request that
// created this transfer. Used to tell a retry from a reused idempotency key.
func (t Transfer) MatchesRequest(req TransferRequest) bool {
	return false // TODO: implement
}

// LedgerEntries returns the double-entry pair for a processed transfer.
func (t Transfer) LedgerEntries() []LedgerEntry {
	return nil // TODO: implement
}
