// Package service holds the transfer workflow: validation, the idempotency
// decision, and the transaction boundary. It contains no SQL and knows nothing
// about HTTP — it decides what happens and in what order, while the repository
// decides how a row is read or written.
package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/domain"
	"github.com/sagarsuperuser/wallet-transfer-assignment/internal/repository"
)

// Result is the outcome of a transfer request.
//
// A transfer that failed for insufficient funds is a Result, not an error: the
// workflow ran to completion and recorded an answer. Only conditions that
// produced no transfer at all — a malformed request, an unknown wallet,
// contention — come back as errors. Keeping the outcome on the transfer is what
// lets a replay report it identically.
type Result struct {
	Transfer domain.Transfer

	// Replayed reports that this request did not create the transfer: an
	// earlier request carrying the same idempotency key did.
	Replayed bool
}

// Transfers runs the transfer workflow.
type Transfers struct {
	store  *repository.Store
	logger *slog.Logger
}

// NewTransfers returns a service backed by store. A nil logger disables logging.
func NewTransfers(store *repository.Store, logger *slog.Logger) *Transfers {
	return &Transfers{store: store, logger: logger}
}

// Create executes a transfer, or returns the transfer that an earlier request
// with the same idempotency key already produced.
//
// The whole workflow is one transaction:
//
//  1. Claim the idempotency key. Losing the claim means another request owns
//     it, so its transfer is returned and no wallet is ever locked.
//  2. Lock both wallets, in a consistent order.
//  3. If the source cannot cover the amount, record FAILED and commit.
//  4. Otherwise move the money, write both ledger entries, mark PROCESSED.
func (s *Transfers) Create(ctx context.Context, request domain.TransferRequest) (Result, error) {
	if err := request.Validate(); err != nil {
		return Result{}, err
	}

	started := time.Now()

	pending := domain.Transfer{
		ID:             uuid.NewString(),
		IdempotencyKey: request.IdempotencyKey,
		RequestHash:    request.Hash(),
		FromWalletID:   request.FromWalletID,
		ToWalletID:     request.ToWalletID,
		Amount:         request.Amount,
		State:          domain.StatePending,
	}

	var result Result
	err := s.store.WithTx(ctx, func(ctx context.Context, tx *repository.Tx) error {
		var err error
		result, err = s.execute(ctx, tx, pending)
		return err
	})
	if err != nil {
		s.logFailure(ctx, request, time.Since(started), err)
		return Result{}, err
	}

	s.logOutcome(ctx, result, time.Since(started))

	return result, nil
}

// execute runs inside the transaction. Returning nil commits — which is how a
// FAILED transfer is made durable rather than rolled back with the money.
func (s *Transfers) execute(ctx context.Context, tx *repository.Tx, pending domain.Transfer) (Result, error) {
	claimed, won, err := tx.ClaimTransfer(ctx, pending)
	if err != nil {
		return Result{}, err
	}
	if !won {
		return replay(ctx, tx, pending)
	}

	wallets, err := tx.LockWalletsInOrder(ctx, claimed.FromWalletID, claimed.ToWalletID)
	if err != nil {
		return Result{}, err
	}

	source, ok := wallets[claimed.FromWalletID]
	if !ok {
		// Unreachable: the claim's foreign keys guarantee both wallets exist,
		// and neither row can be removed while this transfer references it.
		return Result{}, fmt.Errorf("source wallet %s missing after locking", claimed.FromWalletID)
	}

	// Checked under the lock, so the balance read here is the balance written
	// against. The CHECK constraint on wallets.balance is only a backstop: a
	// constraint violation would abort the transaction and take the FAILED row
	// with it.
	if !source.CanCover(claimed.Amount) {
		return fail(ctx, tx, claimed)
	}

	return settle(ctx, tx, claimed)
}

// replay returns the transfer that already owns this idempotency key.
//
// A key is only refused when a committed transfer holds it, so the transfer
// read here has always reached a terminal state: a caller can never be handed
// another request's PENDING.
func replay(ctx context.Context, tx *repository.Tx, pending domain.Transfer) (Result, error) {
	existing, err := tx.TransferByIdempotencyKey(ctx, pending.IdempotencyKey)
	if err != nil {
		return Result{}, err
	}

	// Same key, different transfer. Returning the stored transfer would answer
	// a question this caller did not ask, so the request is refused instead.
	if existing.RequestHash != pending.RequestHash {
		return Result{}, domain.ErrIdempotencyKeyConflict
	}

	return Result{Transfer: existing, Replayed: true}, nil
}

// fail records a transfer that cannot succeed, and commits it.
//
// Committing is the point. A rollback would release the idempotency key, so a
// retry could re-attempt the debit and succeed once the balance changed — one
// key producing two different answers.
func fail(ctx context.Context, tx *repository.Tx, transfer domain.Transfer) (Result, error) {
	if err := transfer.MarkFailed(domain.FailureReasonInsufficientFunds); err != nil {
		return Result{}, err
	}
	if err := tx.MarkTransferFailed(ctx, transfer.ID, transfer.FailureReason); err != nil {
		return Result{}, err
	}

	return Result{Transfer: transfer}, nil
}

// settle moves the money, records both ledger entries, and marks the transfer
// processed. Every step runs in the caller's transaction, so the balances and
// the ledger that explains them commit together or not at all.
func settle(ctx context.Context, tx *repository.Tx, transfer domain.Transfer) (Result, error) {
	if err := tx.DebitWallet(ctx, transfer.FromWalletID, transfer.Amount); err != nil {
		return Result{}, err
	}
	if err := tx.CreditWallet(ctx, transfer.ToWalletID, transfer.Amount); err != nil {
		return Result{}, err
	}
	if err := tx.InsertLedgerEntries(ctx, domain.LedgerEntriesFor(transfer)); err != nil {
		return Result{}, err
	}

	if err := transfer.MarkProcessed(); err != nil {
		return Result{}, err
	}
	if err := tx.MarkTransferProcessed(ctx, transfer.ID); err != nil {
		return Result{}, err
	}

	return Result{Transfer: transfer}, nil
}

// logOutcome records one line per completed request: enough to answer "what
// happened to this transfer" and "is this caller retrying" from logs alone.
func (s *Transfers) logOutcome(ctx context.Context, result Result, elapsed time.Duration) {
	if s.logger == nil {
		return
	}

	s.logger.InfoContext(ctx, "transfer completed",
		slog.String("transfer_id", result.Transfer.ID),
		slog.String("idempotency_key", result.Transfer.IdempotencyKey),
		slog.String("from_wallet_id", result.Transfer.FromWalletID),
		slog.String("to_wallet_id", result.Transfer.ToWalletID),
		slog.Int64("amount", result.Transfer.Amount),
		slog.String("state", string(result.Transfer.State)),
		slog.String("failure_reason", result.Transfer.FailureReason),
		slog.Bool("replayed", result.Replayed),
		slog.Duration("elapsed", elapsed),
	)
}

// logFailure records a request that produced no transfer.
//
// No transfer id: the one generated before the claim may name a row that was
// never written — a validation failure, an unknown wallet, or a claim that lost.
// On a key conflict a transfer does exist, but with a different id, so logging
// the generated one would point at a UUID that never existed. The idempotency
// key is the useful handle anyway, since it is what the caller holds.
func (s *Transfers) logFailure(
	ctx context.Context,
	request domain.TransferRequest,
	elapsed time.Duration,
	err error,
) {
	if s.logger == nil {
		return
	}

	// Contention, an unknown wallet and a reused key are ordinary answers to a
	// well-formed request rather than faults in the service, so they do not
	// deserve the level that a genuine surprise does.
	level := slog.LevelError
	if errors.Is(err, domain.ErrWalletBusy) ||
		errors.Is(err, domain.ErrWalletNotFound) ||
		errors.Is(err, domain.ErrIdempotencyKeyConflict) {
		level = slog.LevelWarn
	}

	s.logger.Log(ctx, level, "transfer failed",
		slog.String("idempotency_key", request.IdempotencyKey),
		slog.String("from_wallet_id", request.FromWalletID),
		slog.String("to_wallet_id", request.ToWalletID),
		slog.Int64("amount", request.Amount),
		slog.Duration("elapsed", elapsed),
		slog.String("error", err.Error()),
	)
}
