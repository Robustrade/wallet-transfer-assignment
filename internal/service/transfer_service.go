// Package service contains the application's business logic: transfer
// orchestration, idempotency handling, and wallet management. It depends
// only on the repository interfaces and the domain package, never on HTTP
// or a specific database driver.
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/repository"
)

// TransferService orchestrates the wallet transfer workflow: idempotency
// claiming, wallet locking, balance updates, ledger writes, and transfer
// state transitions, all inside a single atomic unit of work.
type TransferService struct {
	uow    repository.UnitOfWork
	now    func() time.Time
	newID  func() string
	logger *slog.Logger
}

// Option customizes a TransferService, mainly to allow deterministic
// clocks/IDs in tests.
type Option func(*TransferService)

// WithClock overrides the time source used for timestamps.
func WithClock(now func() time.Time) Option {
	return func(s *TransferService) { s.now = now }
}

// WithIDGenerator overrides how transfer and ledger entry IDs are generated.
func WithIDGenerator(newID func() string) Option {
	return func(s *TransferService) { s.newID = newID }
}

// WithLogger overrides the logger used for observability.
func WithLogger(logger *slog.Logger) Option {
	return func(s *TransferService) { s.logger = logger }
}

// NewTransferService constructs a TransferService backed by uow.
func NewTransferService(uow repository.UnitOfWork, opts ...Option) *TransferService {
	s := &TransferService{
		uow:    uow,
		now:    func() time.Time { return time.Now().UTC() },
		newID:  uuid.NewString,
		logger: slog.Default(),
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// CreateTransferInput is the validated request to move funds between two
// wallets.
type CreateTransferInput struct {
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
}

// CreateTransferResult is the outcome of processing a transfer request.
type CreateTransferResult struct {
	Transfer      domain.Transfer
	LedgerEntries []domain.LedgerEntry
	// Status is the HTTP status code the original request was answered
	// with (e.g. 201 Created for a processed transfer, 422 for
	// insufficient funds), preserved so replays return the same code.
	Status int
	// Replayed is true when this result was served from a previous
	// identical request rather than freshly executed.
	Replayed bool
}

// transferResponseDoc is the canonical JSON shape persisted for idempotent
// replay and returned to callers. Keeping it separate from domain.Transfer
// decouples the wire format from internal field layout.
type transferResponseDoc struct {
	Transfer domain.Transfer      `json:"transfer"`
	Ledger   []domain.LedgerEntry `json:"ledgerEntries"`
}

// CreateTransfer executes (or replays) a wallet-to-wallet transfer.
//
// Idempotency: idempotencyKey is claimed transactionally before any wallet
// mutation happens. A duplicate request with the same key and same
// (fromWalletId, toWalletId, amount) replays the original response. A
// duplicate with the same key but different payload returns
// domain.ErrIdempotencyKeyConflict. If another request with the same key is
// still in flight, domain.ErrIdempotencyInProgress is returned so the
// caller can retry shortly.
//
// Concurrency: both wallets are locked with SELECT ... FOR UPDATE in a
// consistent ascending-ID order within a single transaction, which
// prevents lost updates on concurrent transfers and avoids deadlocks that
// row-locking wallets in mismatched order could cause.
//
// Validation failures that occur before a transfer is durably recorded
// (missing wallet, same-wallet transfer, non-positive amount) abort the
// transaction so the idempotency key is never consumed, letting the caller
// correct the request and retry with the same key. Insufficient funds, by
// contrast, is a legitimate terminal business outcome: it is recorded as a
// FAILED transfer (with no ledger entries) and the idempotency key IS
// consumed, since retrying an unmodified request would fail identically.
func (s *TransferService) CreateTransfer(ctx context.Context, in CreateTransferInput) (*CreateTransferResult, error) {
	if err := in.Validate(); err != nil {
		return nil, err
	}

	requestHash := hashTransferRequest(in)

	var result *CreateTransferResult
	err := s.uow.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		claimed, existing, err := repos.Idempotency.Claim(ctx, in.IdempotencyKey, requestHash)
		if err != nil {
			return fmt.Errorf("claim idempotency key: %w", err)
		}
		if !claimed {
			if existing.RequestHash != requestHash {
				return domain.ErrIdempotencyKeyConflict
			}
			if !existing.Completed() {
				return domain.ErrIdempotencyInProgress
			}
			var doc transferResponseDoc
			if err := json.Unmarshal(existing.ResponseBody, &doc); err != nil {
				return fmt.Errorf("decode replayed response: %w", err)
			}
			result = &CreateTransferResult{Transfer: doc.Transfer, LedgerEntries: doc.Ledger, Status: existing.ResponseStatus, Replayed: true}
			return nil
		}

		res, err := s.executeTransfer(ctx, repos, in)
		if err != nil {
			return err
		}
		result = res
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// executeTransfer performs the fund movement for a freshly claimed
// idempotency key. It assumes it is running inside the caller's
// transaction.
func (s *TransferService) executeTransfer(ctx context.Context, repos repository.Repositories, in CreateTransferInput) (*CreateTransferResult, error) {
	fromWallet, toWallet, err := lockWalletsInOrder(ctx, repos.Wallets, in.FromWalletID, in.ToWalletID)
	if err != nil {
		return nil, err
	}

	now := s.now()
	transfer := domain.Transfer{
		ID:             s.newID(),
		IdempotencyKey: in.IdempotencyKey,
		FromWalletID:   in.FromWalletID,
		ToWalletID:     in.ToWalletID,
		Amount:         in.Amount,
		State:          domain.TransferPending,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if err := repos.Transfers.Create(ctx, &transfer); err != nil {
		return nil, fmt.Errorf("create transfer: %w", err)
	}

	if !fromWallet.CanDebit(in.Amount) {
		if err := transfer.Transition(domain.TransferFailed, "insufficient funds"); err != nil {
			return nil, err
		}
		if err := repos.Transfers.UpdateState(ctx, transfer.ID, transfer.State, transfer.FailureReason); err != nil {
			return nil, fmt.Errorf("update transfer state: %w", err)
		}
		return s.finalize(ctx, repos, transfer, nil, http.StatusUnprocessableEntity)
	}

	entries := []domain.LedgerEntry{
		{ID: s.newID(), WalletID: fromWallet.ID, TransferID: transfer.ID, Type: domain.LedgerEntryDebit, Amount: in.Amount, CreatedAt: now},
		{ID: s.newID(), WalletID: toWallet.ID, TransferID: transfer.ID, Type: domain.LedgerEntryCredit, Amount: in.Amount, CreatedAt: now},
	}

	if err := repos.Wallets.UpdateBalance(ctx, fromWallet.ID, fromWallet.Balance-in.Amount); err != nil {
		return nil, fmt.Errorf("debit wallet: %w", err)
	}
	if err := repos.Wallets.UpdateBalance(ctx, toWallet.ID, toWallet.Balance+in.Amount); err != nil {
		return nil, fmt.Errorf("credit wallet: %w", err)
	}
	if err := repos.Ledger.CreateEntries(ctx, entries); err != nil {
		return nil, fmt.Errorf("write ledger entries: %w", err)
	}
	if err := transfer.Transition(domain.TransferProcessed, ""); err != nil {
		return nil, err
	}
	if err := repos.Transfers.UpdateState(ctx, transfer.ID, transfer.State, ""); err != nil {
		return nil, fmt.Errorf("update transfer state: %w", err)
	}

	return s.finalize(ctx, repos, transfer, entries, http.StatusCreated)
}

// finalize stores the durable idempotent response for transfer and returns
// the result to the caller.
func (s *TransferService) finalize(ctx context.Context, repos repository.Repositories, transfer domain.Transfer, entries []domain.LedgerEntry, status int) (*CreateTransferResult, error) {
	body, err := json.Marshal(transferResponseDoc{Transfer: transfer, Ledger: entries})
	if err != nil {
		return nil, fmt.Errorf("encode response for idempotency store: %w", err)
	}
	if err := repos.Idempotency.Complete(ctx, transfer.IdempotencyKey, transfer.ID, status, body); err != nil {
		return nil, fmt.Errorf("complete idempotency record: %w", err)
	}
	return &CreateTransferResult{Transfer: transfer, LedgerEntries: entries, Status: status}, nil
}

// lockWalletsInOrder fetches both wallets under a row lock in a
// deterministic ascending-ID order to prevent deadlocks between concurrent
// transfers that touch the same two wallets in opposite directions.
func lockWalletsInOrder(ctx context.Context, wallets repository.WalletRepository, fromID, toID string) (from, to *domain.Wallet, err error) {
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

// hashTransferRequest produces a stable fingerprint of the fields that must
// match for an idempotency key to be replayed safely.
func hashTransferRequest(in CreateTransferInput) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", in.FromWalletID, in.ToWalletID, in.Amount)))
	return hex.EncodeToString(sum[:])
}

// GetTransfer returns a transfer and its ledger entries by ID.
func (s *TransferService) GetTransfer(ctx context.Context, id string) (*CreateTransferResult, error) {
	var result *CreateTransferResult
	err := s.uow.Execute(ctx, func(ctx context.Context, repos repository.Repositories) error {
		transfer, err := repos.Transfers.GetByID(ctx, id)
		if err != nil {
			return err
		}
		entries, err := repos.Ledger.ListByTransfer(ctx, id)
		if err != nil {
			return err
		}
		result = &CreateTransferResult{Transfer: *transfer, LedgerEntries: entries}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
