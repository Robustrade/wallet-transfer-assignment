package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"wallettransfer/internal/domain"
)

// Transaction is the minimal persistence contract needed by the business flow.
// Every method runs in a single database transaction managed by TransferStore.
type Transaction interface {
	Claim(ctx context.Context, request domain.TransferRequest) (id string, previous *domain.TransferResult, err error)
	LockWallet(ctx context.Context, id string) (balance int64, err error)
	DebitIfEnough(ctx context.Context, id string, amount int64) (bool, error)
	Credit(ctx context.Context, id string, amount int64) error
	PostLedger(ctx context.Context, transferID, fromID, toID string, amount int64) error
	Finish(ctx context.Context, id, status, failureReason string) error
}

type TransferStore interface {
	WithinTransaction(ctx context.Context, fn func(Transaction) error) error
}

type Service struct{ store TransferStore }

func New(store TransferStore) *Service { return &Service{store: store} }

const maxTransactionAttempts = 3

// Transfer orchestrates one synchronous, all-or-nothing financial transaction.
// Known aborted transactions are retried up to three times. Unknown COMMIT
// outcomes are returned as errors; the client retries with the same key.
func (s *Service) Transfer(ctx context.Context, request domain.TransferRequest) (domain.TransferResult, error) {
	if err := request.ValidateStructure(); err != nil {
		return domain.TransferResult{}, err
	}
	for attempt := 0; attempt < maxTransactionAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return domain.TransferResult{}, err
		}
		result, err := s.transferOnce(ctx, request)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, domain.ErrRetryableTransaction) || attempt == maxTransactionAttempts-1 {
			return domain.TransferResult{}, err
		}
		// A bounded, increasing delay helps avoid immediately contending again.
		// The caller's context still controls the overall time budget.
		delay := time.Duration(10*(1<<attempt)) * time.Millisecond
		wait := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			wait.Stop()
			return domain.TransferResult{}, ctx.Err()
		case <-wait.C:
		}
	}
	return domain.TransferResult{}, domain.ErrInvariantViolation // unreachable
}

func (s *Service) transferOnce(ctx context.Context, request domain.TransferRequest) (domain.TransferResult, error) {
	var result domain.TransferResult
	err := s.store.WithinTransaction(ctx, func(tx Transaction) error {
		id, previous, err := tx.Claim(ctx, request)
		if err != nil {
			return err
		}
		if previous != nil {
			result = *previous
			return nil
		}

		// Lock both wallets in the same order for A->B and B->A requests.
		first, second := request.FromWalletID, request.ToWalletID
		if first > second {
			first, second = second, first
		}
		firstBalance, err := tx.LockWallet(ctx, first)
		if err != nil {
			return err
		}
		secondBalance, err := tx.LockWallet(ctx, second)
		if err != nil {
			return err
		}
		fromBalance := firstBalance
		if request.FromWalletID == second {
			fromBalance = secondBalance
		}

		if fromBalance < request.Amount {
			if err := tx.Finish(ctx, id, domain.StatusFailed, domain.ReasonInsufficientFunds); err != nil {
				return err
			}
			result = domain.TransferResult{ID: id, Status: domain.StatusFailed, FailureReason: domain.ReasonInsufficientFunds}
			return nil
		}

		// The conditional debit remains a database-enforced safety check even
		// though the application has already locked and checked the balance.
		debited, err := tx.DebitIfEnough(ctx, request.FromWalletID, request.Amount)
		if err != nil {
			return err
		}
		if !debited {
			return fmt.Errorf("sender balance changed after row lock: %w", domain.ErrInvariantViolation)
		}
		if err := tx.Credit(ctx, request.ToWalletID, request.Amount); err != nil {
			return err
		}
		if err := tx.PostLedger(ctx, id, request.FromWalletID, request.ToWalletID, request.Amount); err != nil {
			return err
		}
		if err := tx.Finish(ctx, id, domain.StatusProcessed, ""); err != nil {
			return err
		}

		result = domain.TransferResult{ID: id, Status: domain.StatusProcessed}
		return nil
	})
	if err != nil {
		return domain.TransferResult{}, err
	}
	return result, nil
}
