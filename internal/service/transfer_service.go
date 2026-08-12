package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

// Outcome describes how a Create call resolved: a brand new transfer, or a
// replay of one that already existed.
type Outcome string

const (
	OutcomeCreated  Outcome = "created"
	OutcomeReplayed Outcome = "replayed"
)

// TransferResult is returned by TransferService.Create.
type TransferResult struct {
	Transfer domain.Transfer
	Outcome  Outcome
}

// TransferService orchestrates idempotent, transactional wallet transfers.
// It is the only place in the codebase that decides what "atomic" means for
// a transfer — see docs/design.md §5 for the full algorithm this
// implements.
type TransferService struct {
	uow   UnitOfWork
	now   func() time.Time
	newID func() (uuid.UUID, error)
}

// NewTransferService builds a TransferService backed by uow.
func NewTransferService(uow UnitOfWork) *TransferService {
	return &TransferService{
		uow:   uow,
		now:   time.Now,
		newID: uuid.NewV7,
	}
}

// Get returns a transfer by id.
func (s *TransferService) Get(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	return s.uow.Transfers().FindByID(ctx, id)
}

// Create settles cmd, or replays the result of an earlier call with the same
// idempotency key. See docs/design.md §5 and §6.
func (s *TransferService) Create(ctx context.Context, cmd domain.TransferCommand) (TransferResult, error) {
	if err := cmd.Validate(); err != nil {
		return TransferResult{}, err
	}

	// Fast path: a key we've already seen doesn't need a transaction or a
	// lock at all.
	if existing, err := s.uow.Transfers().FindByIdempotencyKey(ctx, cmd.IdempotencyKey); err == nil {
		return replay(existing, cmd)
	} else if !errors.Is(err, domain.ErrTransferNotFound) {
		return TransferResult{}, err
	}

	var result TransferResult
	err := s.uow.Do(ctx, func(ctx context.Context, r Repos) error {
		res, err := s.settle(ctx, r, cmd)
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

// settle is the body of the single transaction described in
// docs/design.md §5.2: lock both wallets, validate currency, claim the
// idempotency key, then either mark the transfer FAILED (insufficient
// funds) or move the money and mark it PROCESSED. Split out of Create to
// keep each step's branching independently readable.
func (s *TransferService) settle(ctx context.Context, r Repos, cmd domain.TransferCommand) (TransferResult, error) {
	fromWallet, toWallet, err := lockWalletsInOrder(ctx, r.Wallets(), cmd.FromWalletID, cmd.ToWalletID)
	if err != nil {
		return TransferResult{}, err
	}
	if err := checkCurrency(fromWallet, toWallet, cmd.Currency); err != nil {
		return TransferResult{}, err
	}

	id, err := s.newID()
	if err != nil {
		return TransferResult{}, err
	}
	transfer := domain.NewPendingTransfer(id, cmd, fromWallet.Currency, s.now())

	inserted, err := r.Transfers().InsertIfAbsent(ctx, transfer)
	if err != nil {
		return TransferResult{}, err
	}
	if !inserted {
		// Lost the race to claim the key: a concurrent request beat us to
		// it. Its outcome is now visible (InsertIfAbsent only returns once
		// any conflicting transaction has resolved).
		existing, err := r.Transfers().FindByIdempotencyKey(ctx, cmd.IdempotencyKey)
		if err != nil {
			return TransferResult{}, err
		}
		return replay(existing, cmd)
	}

	if !fromWallet.CanDebit(cmd.AmountMinor) {
		return s.failInsufficientFunds(ctx, r, transfer)
	}
	return s.moveFunds(ctx, r, transfer, fromWallet, toWallet, cmd.AmountMinor)
}

func checkCurrency(fromWallet, toWallet domain.Wallet, requested string) error {
	if fromWallet.Currency != toWallet.Currency {
		return domain.ErrCurrencyMismatch
	}
	if requested != "" && requested != fromWallet.Currency {
		return domain.ErrCurrencyMismatch
	}
	return nil
}

func (s *TransferService) failInsufficientFunds(ctx context.Context, r Repos, transfer domain.Transfer) (TransferResult, error) {
	const msg = "source wallet balance is lower than the transfer amount"
	if err := r.Transfers().MarkFailed(ctx, transfer.ID, domain.FailureCodeInsufficientFunds, msg); err != nil {
		return TransferResult{}, err
	}
	transfer.Status = domain.TransferStatusFailed
	transfer.FailureCode = domain.FailureCodeInsufficientFunds
	transfer.FailureMessage = msg
	return TransferResult{Transfer: transfer, Outcome: OutcomeCreated}, nil
}

// moveFunds performs the guarded debit/credit, writes the double-entry
// ledger pair, and marks the transfer PROCESSED, all inside the caller's
// transaction.
func (s *TransferService) moveFunds(ctx context.Context, r Repos, transfer domain.Transfer, fromWallet, toWallet domain.Wallet, amount int64) (TransferResult, error) {
	fromBalanceAfter, err := r.Wallets().ApplyDelta(ctx, fromWallet.ID, -amount)
	if err != nil {
		return TransferResult{}, err
	}
	toBalanceAfter, err := r.Wallets().ApplyDelta(ctx, toWallet.ID, amount)
	if err != nil {
		return TransferResult{}, err
	}

	debit, credit := domain.NewDoubleEntry(transfer.ID, fromWallet.ID, toWallet.ID, amount, transfer.Currency, fromBalanceAfter, toBalanceAfter)
	if err := r.Ledger().InsertPair(ctx, debit, credit); err != nil {
		return TransferResult{}, err
	}

	settledAt := s.now()
	if err := r.Transfers().MarkProcessed(ctx, transfer.ID, settledAt); err != nil {
		return TransferResult{}, err
	}

	transfer.Status = domain.TransferStatusProcessed
	transfer.SettledAt = &settledAt
	return TransferResult{Transfer: transfer, Outcome: OutcomeCreated}, nil
}

// lockWalletsInOrder locks both wallets FOR NO KEY UPDATE in ascending id
// order, in two separate statements, then returns them in (from, to) order
// regardless of which was locked first. See docs/design.md §7: this
// deterministic ordering, issued as two statements rather than one sorted
// multi-row lock, is what makes concurrent A->B and B->A transfers
// deadlock-free.
func lockWalletsInOrder(ctx context.Context, wallets WalletRepository, fromID, toID string) (from, to domain.Wallet, err error) {
	firstID, secondID := fromID, toID
	if secondID < firstID {
		firstID, secondID = secondID, firstID
	}

	first, err := wallets.GetForUpdate(ctx, firstID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}
	second, err := wallets.GetForUpdate(ctx, secondID)
	if err != nil {
		return domain.Wallet{}, domain.Wallet{}, err
	}

	if first.ID == fromID {
		return first, second, nil
	}
	return second, first, nil
}

// replay validates that a found transfer matches the incoming command and
// returns it as a replayed result. See docs/design.md §6.
func replay(existing domain.Transfer, cmd domain.TransferCommand) (TransferResult, error) {
	if existing.RequestHash != cmd.Fingerprint() {
		return TransferResult{}, domain.ErrIdempotencyKeyReuse
	}
	if existing.Status == domain.TransferStatusPending {
		return TransferResult{}, domain.ErrTransferInProgress
	}
	return TransferResult{Transfer: existing, Outcome: OutcomeReplayed}, nil
}
