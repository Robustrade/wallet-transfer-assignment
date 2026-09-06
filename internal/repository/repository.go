package repository

import (
	"context"

	"wallet-transfer/internal/domain"

	"github.com/google/uuid"
)

// TransferRepository owns all SQL access and transaction/locking strategy
// for the transfer workflow. There is exactly one method on the write path
// (ExecuteTransfer) so that the transaction boundary is a single, auditable
// unit rather than something assembled ad hoc by the service layer.
type TransferRepository interface {
	// ExecuteTransfer runs the full transfer attempt — idempotency bookkeeping,
	// wallet locking, balance validation, balance updates, ledger inserts, and
	// the state transition — inside one database transaction. It returns the
	// resulting transfer and idempotency outcome (see ExecutionResult).
	ExecuteTransfer(ctx context.Context, req domain.CreateTransferRequest) (*ExecutionResult, error)
}

// ExecutionResult communicates back to the service layer both what happened
// with the transfer itself and how it relates to idempotency: whether this
// call actually performed the work, or replayed/rejected based on a prior
// idempotency record.
type ExecutionResult struct {
	Transfer *domain.Transfer

	// Outcome describes how this result was produced.
	Outcome ExecutionOutcome

	// Set only when Outcome == OutcomeConflict.
	ConflictReason string
}

type ExecutionOutcome string

const (
	// OutcomeExecuted means this call performed the transfer (success or
	// domain-level failure such as insufficient funds) for the first time.
	OutcomeExecuted ExecutionOutcome = "EXECUTED"
	// OutcomeReplayed means an idempotency record already existed for this
	// key+request and its stored result was returned without re-executing.
	OutcomeReplayed ExecutionOutcome = "REPLAYED"
	// OutcomeConflict means the idempotency key was reused with different
	// request parameters, or a request with the same key is currently in
	// flight.
	OutcomeConflict ExecutionOutcome = "CONFLICT"
)

// WalletRepository provides read access to wallets, used for endpoints
// outside the core transfer flow (e.g. checking a balance).
type WalletRepository interface {
	GetWallet(ctx context.Context, id uuid.UUID) (*domain.Wallet, error)
	CreateWallet(ctx context.Context, name string, initialBalanceCents int64) (*domain.Wallet, error)
}
