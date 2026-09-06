// Package service contains the business logic and transactional
// orchestration for wallet transfers: idempotency handling, the
// lock-ordering/concurrency strategy, and the transfer state machine
// invocation. It has no HTTP or SQL concerns.
package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
)

type TransferRequest struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// TransferResult wraps the outcome transfer plus whether this call replayed
// a previously committed result rather than executing anything new.
type TransferResult struct {
	Transfer *domain.Transfer
	Replayed bool
}

type TransferService struct {
	uow   repository.UnitOfWork
	idGen func() string
	now   func() time.Time
}

func NewTransferService(uow repository.UnitOfWork) *TransferService {
	return &TransferService{
		uow:   uow,
		idGen: func() string { return uuid.NewString() },
		now:   time.Now,
	}
}

// Execute runs a wallet transfer, or, if idempotencyKey was already used for
// an identical request, returns the previously recorded outcome without
// mutating any balances again.
func (s *TransferService) Execute(ctx context.Context, req TransferRequest) (*TransferResult, error) {
	if req.IdempotencyKey == "" {
		return nil, domain.ErrIdempotencyKeyRequired
	}
	if req.Amount <= 0 {
		return nil, domain.ErrInvalidAmount
	}
	if req.FromWalletID == req.ToWalletID {
		return nil, domain.ErrSameWallet
	}

	requestHash := hashRequest(req.FromWalletID, req.ToWalletID, req.Amount)
	now := s.now()

	transfer, err := domain.NewTransfer(s.idGen(), req.IdempotencyKey, req.FromWalletID, req.ToWalletID, req.Amount, requestHash, now)
	if err != nil {
		return nil, err
	}

	var result TransferResult
	err = s.uow.RunWriteTx(ctx, func(ctx context.Context, repos repository.Repos) error {
		// Validate both wallets exist before inserting the transfer row: the
		// FK constraint would also reject a missing wallet, but only as a
		// generic constraint-failed error, not domain.ErrWalletNotFound.
		if _, err := repos.Wallets.Get(ctx, req.FromWalletID); err != nil {
			return err
		}
		if _, err := repos.Wallets.Get(ctx, req.ToWalletID); err != nil {
			return err
		}

		insertErr := repos.Transfers.Insert(ctx, transfer)
		if insertErr != nil {
			if errors.Is(insertErr, repository.ErrIdempotencyKeyExists) {
				existing, getErr := repos.Transfers.GetByIdempotencyKey(ctx, req.IdempotencyKey)
				if getErr != nil {
					return fmt.Errorf("load existing transfer for idempotency key: %w", getErr)
				}
				if existing.RequestHash != requestHash {
					return domain.ErrIdempotencyKeyConflict
				}
				result = TransferResult{Transfer: existing, Replayed: true}
				return nil
			}
			return insertErr
		}

		processed, execErr := s.executeLocked(ctx, repos, transfer)
		if execErr != nil {
			return execErr
		}
		result = TransferResult{Transfer: processed, Replayed: false}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

// executeLocked performs the balance mutation for a freshly inserted PENDING
// transfer. It must run inside the same transaction that inserted it.
func (s *TransferService) executeLocked(ctx context.Context, repos repository.Repos, t *domain.Transfer) (*domain.Transfer, error) {
	wallets, err := lockWalletsInOrder(ctx, repos, t.FromWalletID, t.ToWalletID)
	if err != nil {
		return nil, err
	}
	from, to := wallets[t.FromWalletID], wallets[t.ToWalletID]

	now := s.now()

	if !from.CanDebit(t.Amount) {
		if err := t.MarkFailed("insufficient funds", now); err != nil {
			return nil, err
		}
		if err := repos.Transfers.UpdateStatus(ctx, t); err != nil {
			return nil, err
		}
		return t, nil
	}

	if err := repos.Wallets.UpdateBalance(ctx, from.ID, from.Balance-t.Amount, now); err != nil {
		return nil, err
	}
	if err := repos.Wallets.UpdateBalance(ctx, to.ID, to.Balance+t.Amount, now); err != nil {
		return nil, err
	}

	entries := []domain.LedgerEntry{
		{ID: s.idGen(), TransferID: t.ID, WalletID: t.FromWalletID, Type: domain.LedgerEntryDebit, Amount: t.Amount, CreatedAt: now},
		{ID: s.idGen(), TransferID: t.ID, WalletID: t.ToWalletID, Type: domain.LedgerEntryCredit, Amount: t.Amount, CreatedAt: now},
	}
	if err := repos.Ledger.InsertEntries(ctx, entries); err != nil {
		return nil, err
	}

	if err := t.MarkProcessed(now); err != nil {
		return nil, err
	}
	if err := repos.Transfers.UpdateStatus(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// lockWalletsInOrder locks both wallets for update within the current
// transaction, always in ascending ID order regardless of which is the
// debit/credit side. This is the deadlock-avoidance strategy: two concurrent
// transfers moving money in opposite directions between the same pair of
// wallets will always attempt to acquire locks in the same order, so one
// blocks behind the other instead of each holding a lock the other needs.
func lockWalletsInOrder(ctx context.Context, repos repository.Repos, walletA, walletB string) (map[string]*domain.Wallet, error) {
	first, second := walletA, walletB
	if second < first {
		first, second = second, first
	}

	firstWallet, err := repos.Wallets.LockForUpdate(ctx, first)
	if err != nil {
		return nil, err
	}
	secondWallet, err := repos.Wallets.LockForUpdate(ctx, second)
	if err != nil {
		return nil, err
	}

	return map[string]*domain.Wallet{
		first:  firstWallet,
		second: secondWallet,
	}, nil
}
