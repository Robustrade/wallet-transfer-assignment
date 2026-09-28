package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LokilokeshM/wallet-transfer-assignment/internal/domain"
)

// TransferService moves money between wallets with exactly-once semantics
// per idempotency key.
type TransferService struct {
	tx  TxManager
	now func() time.Time
}

// NewTransferService returns a TransferService that runs each transfer in a
// transaction from tx.
func NewTransferService(tx TxManager) *TransferService {
	return &TransferService{tx: tx, now: time.Now}
}

// CreateTransfer executes cmd, or replays the stored result if the key was
// already used with the same payload.
//
// Everything happens in ONE transaction: the idempotency claim, the transfer
// row, both balance updates, both ledger entries and the final status. The
// transaction either commits all of them or none. So:
//   - a crash or cancellation at any point leaves no partial transfer and
//     no orphan idempotency key, and the client can safely retry;
//   - a committed transfer is always visible to later requests with the
//     same key, even across restarts.
//
// Returned errors:
//   - *ValidationError (matches domain.ErrValidation): bad input, nothing stored
//   - domain.ErrWalletNotFound: nothing stored, key can be reused
//   - domain.ErrIdempotencyKeyReused: key belongs to a different request
//   - ErrUnavailable: transient infrastructure failure, nothing stored
//
// Business-rule failures (insufficient funds, currency mismatch, overflow)
// are NOT errors. They return a FAILED transfer that is stored and replayed
// like any other outcome.
func (s *TransferService) CreateTransfer(ctx context.Context, cmd CreateTransferCommand) (TransferResult, error) {
	// The handler validates too; this is defense in depth for other callers.
	if err := cmd.Validate(); err != nil {
		return TransferResult{}, err
	}
	hash := cmd.RequestHash()

	var result TransferResult
	err := s.tx.WithinTx(ctx, func(r Repos) error {
		// WithinTx may retry this function, so compute everything from
		// scratch on each attempt and only publish it via result at the end.
		res, err := s.execute(ctx, r, cmd, hash)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return TransferResult{}, err
	}
	return result, nil
}

// execute is the body of the transaction. See CreateTransfer.
func (s *TransferService) execute(ctx context.Context, r Repos, cmd CreateTransferCommand, hash string) (TransferResult, error) {
	now := s.now()

	// 1. Claim the idempotency key. If another request already holds it, the
	//    database makes us wait until that request's transaction ends.
	inserted, err := r.Idempotency.Insert(ctx, cmd.IdempotencyKey, hash, now)
	if err != nil {
		return TransferResult{}, err
	}
	if !inserted {
		return replay(ctx, r, cmd.IdempotencyKey, hash)
	}

	// 2. Lock both wallets, always in sorted id order. Two opposite
	//    transfers (A->B and B->A) then lock in the same order, and cannot
	//    deadlock each other. A missing wallet returns ErrWalletNotFound,
	//    which rolls back the whole transaction, including the key claimed
	//    above.
	from, to, err := lockWallets(ctx, r.Wallets, cmd.FromWalletID, cmd.ToWalletID)
	if err != nil {
		return TransferResult{}, err
	}

	// 3. Record the transfer as PENDING. Done after the wallet check,
	//    because the transfer row has foreign keys to both wallets.
	transfer, err := domain.NewTransfer(cmd.IdempotencyKey, cmd.FromWalletID, cmd.ToWalletID, cmd.Amount, now)
	if err != nil {
		return TransferResult{}, err
	}
	if err := r.Transfers.Create(ctx, transfer); err != nil {
		return TransferResult{}, err
	}

	// 4. Apply the business rules to the locked wallets.
	fromBefore, toBefore := from.Balance, to.Balance
	if err := domain.ApplyTransfer(from, to, cmd.Amount); err != nil {
		reason, ok := domain.FailureReasonFor(err)
		if !ok {
			return TransferResult{}, err // not a business outcome: roll back
		}
		// Business-rule failure: store it as FAILED, so a retry replays it.
		if err := transfer.MarkFailed(reason); err != nil {
			return TransferResult{}, err
		}
		if err := finalize(ctx, r, transfer, now); err != nil {
			return TransferResult{}, err
		}
		return TransferResult{Transfer: *transfer}, nil
	}

	// 5. Persist the new balances and the double-entry ledger lines.
	if err := r.Wallets.UpdateBalance(ctx, from.ID, fromBefore, from.Balance, now); err != nil {
		return TransferResult{}, err
	}
	if err := r.Wallets.UpdateBalance(ctx, to.ID, toBefore, to.Balance, now); err != nil {
		return TransferResult{}, err
	}
	debit, credit := domain.NewLedgerEntries(transfer)
	if err := r.Ledger.Insert(ctx, debit, credit); err != nil {
		return TransferResult{}, err
	}

	// 6. PENDING -> PROCESSED, then link the key to the transfer.
	if err := transfer.MarkProcessed(); err != nil {
		return TransferResult{}, err
	}
	if err := finalize(ctx, r, transfer, now); err != nil {
		return TransferResult{}, err
	}
	return TransferResult{Transfer: *transfer}, nil
}

// finalize persists the transfer's final status with the guarded update
// (only from PENDING), and links the idempotency record to it.
func finalize(ctx context.Context, r Repos, t *domain.Transfer, now time.Time) error {
	if err := r.Transfers.UpdateStatus(ctx, t, now); err != nil {
		return err
	}
	return r.Idempotency.LinkTransfer(ctx, t.IdempotencyKey, t.ID)
}

// replay returns the stored outcome for a key that already exists, provided
// the payload matches the one it was first used with.
func replay(ctx context.Context, r Repos, key, hash string) (TransferResult, error) {
	rec, err := r.Idempotency.Get(ctx, key)
	if err != nil {
		return TransferResult{}, err
	}
	if rec.RequestHash != hash {
		return TransferResult{}, domain.ErrIdempotencyKeyReused
	}
	if !rec.TransferID.Valid {
		// Unreachable: a record becomes visible to other transactions only
		// when its transaction commits, and every committing path links it
		// first.
		return TransferResult{}, errors.New("idempotency record has no transfer")
	}
	t, err := r.Transfers.GetByID(ctx, rec.TransferID.UUID)
	if err != nil {
		return TransferResult{}, fmt.Errorf("load transfer for replay: %w", err)
	}
	return TransferResult{Transfer: *t, Replayed: true}, nil
}

// lockWallets loads and locks both wallets in ascending id order, and
// returns them as (from, to).
func lockWallets(ctx context.Context, wallets WalletRepository, fromID, toID string) (from, to *domain.Wallet, err error) {
	firstID, secondID := fromID, toID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}
	first, err := wallets.GetForUpdate(ctx, firstID)
	if err != nil {
		return nil, nil, err
	}
	second, err := wallets.GetForUpdate(ctx, secondID)
	if err != nil {
		return nil, nil, err
	}
	if first.ID == fromID {
		return first, second, nil
	}
	return second, first, nil
}
