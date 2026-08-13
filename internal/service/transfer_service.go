package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/robustrade/wallet-transfer-assignment/internal/repository"
)

// TransferService owns the transfer workflow: idempotency, wallet locking,
// balance updates and the double-entry ledger. It depends only on the
// repository interfaces, never on a driver, so it's testable with fakes.
type TransferService struct {
	tx          repository.TxManager
	wallets     repository.WalletRepository
	transfers   repository.TransferRepository
	ledger      repository.LedgerRepository
	idempotency repository.IdempotencyRepository
	newID       func() string
}

func NewTransferService(
	tx repository.TxManager,
	wallets repository.WalletRepository,
	transfers repository.TransferRepository,
	ledger repository.LedgerRepository,
	idempotency repository.IdempotencyRepository,
) *TransferService {
	return &TransferService{
		tx:          tx,
		wallets:     wallets,
		transfers:   transfers,
		ledger:      ledger,
		idempotency: idempotency,
		newID:       func() string { return uuid.NewString() },
	}
}

func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferInput) (*TransferResult, error) {
	if err := validateCreateTransferInput(in); err != nil {
		return nil, err
	}

	hash := requestHash(in)

	_, claimed, err := s.idempotency.Claim(ctx, in.IdempotencyKey, hash)
	if err != nil {
		return nil, fmt.Errorf("claim idempotency key: %w", err)
	}

	if !claimed {
		return s.resolveDuplicate(ctx, in.IdempotencyKey, hash)
	}

	result, err := s.execute(ctx, in)
	if err != nil {
		if errors.Is(err, domain.ErrCommitOutcomeUnknown) {
			// The transfer's commit may have actually landed even though this
			// call saw an error -- releasing the key here could let a retry
			// re-run the transfer against a commit that already succeeded,
			// double-spending. Leave the key PENDING: a retry gets
			// ErrRequestInProgress instead of silently redoing the work. This
			// matches the deliberate policy for a stuck PENDING key (see
			// DESIGN.md's idempotency section) -- surface it rather than risk
			// a duplicate transfer.
			return nil, err
		}
		// Nothing durable was recorded for this attempt (bad wallet, infra
		// error, ...) -- free the key rather than wedging it on a response
		// that was never written.
		if releaseErr := s.idempotency.Release(ctx, in.IdempotencyKey); releaseErr != nil {
			return nil, fmt.Errorf("%w (release also failed: %v)", err, releaseErr)
		}
		return nil, err
	}
	return result, nil
}

// resolveDuplicate handles a key that was already claimed by a previous
// request: replay if it finished, reject if the payload changed, or signal
// "already in progress" if it's still being processed.
func (s *TransferService) resolveDuplicate(ctx context.Context, key, hash string) (*TransferResult, error) {
	rec, err := s.idempotency.Get(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("load idempotency record: %w", err)
	}

	if rec.RequestHash != hash {
		return nil, domain.ErrIdempotencyKeyReused
	}

	if rec.Status == domain.IdempotencyPending {
		return nil, domain.ErrRequestInProgress
	}

	transfer, err := s.transfers.GetByID(ctx, rec.TransferID)
	if err != nil {
		return nil, fmt.Errorf("load replayed transfer: %w", err)
	}
	return transferResult(transfer, true), nil
}

// execute runs the whole transfer -- locking both wallets, checking funds,
// writing the transfer/ledger/balances, and completing the idempotency
// record -- inside one DB transaction so it is all-or-nothing.
func (s *TransferService) execute(ctx context.Context, in CreateTransferInput) (*TransferResult, error) {
	var result *TransferResult

	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		firstID, secondID := lockOrder(in.FromWalletID, in.ToWalletID)

		first, err := s.wallets.GetForUpdate(ctx, firstID)
		if err != nil {
			return err
		}
		second, err := s.wallets.GetForUpdate(ctx, secondID)
		if err != nil {
			return err
		}
		from, to := resolveFromTo(first, second, in.FromWalletID)

		if from.Currency != to.Currency {
			return domain.ErrCurrencyMismatch
		}

		transfer := domain.NewPendingTransfer(s.newID(), in.IdempotencyKey, in.FromWalletID, in.ToWalletID, in.Amount)
		if err := s.transfers.Create(ctx, transfer); err != nil {
			return fmt.Errorf("create transfer: %w", err)
		}

		if !from.HasSufficientFunds(in.Amount) {
			if err := s.failTransfer(ctx, transfer, domain.ErrInsufficientFunds.Error()); err != nil {
				return err
			}
			result = transferResult(transfer, false)
			return nil
		}

		if err := s.settleTransfer(ctx, transfer, from, to, in.Amount); err != nil {
			return err
		}
		result = transferResult(transfer, false)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

func (s *TransferService) failTransfer(ctx context.Context, transfer *domain.Transfer, reason string) error {
	if !transfer.Status.CanTransitionTo(domain.TransferFailed) {
		return domain.ErrInvalidTransition
	}
	transfer.Status = domain.TransferFailed
	transfer.FailureReason = reason

	if err := s.transfers.UpdateStatus(ctx, transfer.ID, domain.TransferFailed, reason); err != nil {
		return fmt.Errorf("mark transfer failed: %w", err)
	}
	if err := s.idempotency.Complete(ctx, transfer.IdempotencyKey, transfer.ID); err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	return nil
}

func (s *TransferService) settleTransfer(ctx context.Context, transfer *domain.Transfer, from, to *domain.Wallet, amount int64) error {
	entries := domain.DoubleEntry(s.newID(), s.newID(), transfer.ID, from.ID, to.ID, amount)
	if err := s.ledger.InsertEntries(ctx, entries[:]); err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}

	if err := s.wallets.UpdateBalance(ctx, from.ID, from.Balance-amount); err != nil {
		return fmt.Errorf("debit wallet: %w", err)
	}
	if err := s.wallets.UpdateBalance(ctx, to.ID, to.Balance+amount); err != nil {
		return fmt.Errorf("credit wallet: %w", err)
	}

	if !transfer.Status.CanTransitionTo(domain.TransferProcessed) {
		return domain.ErrInvalidTransition
	}
	transfer.Status = domain.TransferProcessed

	if err := s.transfers.UpdateStatus(ctx, transfer.ID, domain.TransferProcessed, ""); err != nil {
		return fmt.Errorf("mark transfer processed: %w", err)
	}
	if err := s.idempotency.Complete(ctx, transfer.IdempotencyKey, transfer.ID); err != nil {
		return fmt.Errorf("complete idempotency record: %w", err)
	}
	return nil
}

func (s *TransferService) GetTransfer(ctx context.Context, id string) (*TransferResult, error) {
	t, err := s.transfers.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return transferResult(t, false), nil
}

func validateCreateTransferInput(in CreateTransferInput) error {
	if in.IdempotencyKey == "" {
		return domain.ErrMissingIdempotencyKey
	}
	if in.Amount <= 0 {
		return domain.ErrInvalidAmount
	}
	if in.FromWalletID == in.ToWalletID {
		return domain.ErrSameWallet
	}
	return nil
}

// lockOrder returns the two wallet IDs in a fixed, deterministic order so
// that any two concurrent transfers touching the same pair of wallets --
// regardless of which is "from" and which is "to" in each -- always acquire
// their row locks in the same order. That's what prevents a classic
// A-locks-1-waits-on-2 / B-locks-2-waits-on-1 deadlock.
func lockOrder(a, b string) (string, string) {
	if a < b {
		return a, b
	}
	return b, a
}

func resolveFromTo(first, second *domain.Wallet, fromID string) (from, to *domain.Wallet) {
	if first.ID == fromID {
		return first, second
	}
	return second, first
}
