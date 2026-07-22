// Package mock provides configurable in-memory fakes of the repository ports and
// other seams, so the service and worker can be unit-tested without a database
// and every error branch can be injected.
package mock

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"wallet-transfer/internal/domain"
	"wallet-transfer/internal/repository"
)

// Store is a fake repository.Store. Unset hooks return zero values / nil.
type Store struct {
	EnqueueFn func(context.Context, domain.NewTransferParams) (domain.Transfer, bool, error)
	GetFn     func(context.Context, uuid.UUID) (domain.Transfer, error)
	ClaimFn   func(context.Context, string, int) ([]domain.Transfer, error)
	ReapFn    func(context.Context, time.Duration) (int64, error)

	// TxStore is passed to fn by the default Tx. TxErr is returned after fn
	// succeeds (to simulate a commit failure). TxFn overrides Tx entirely.
	TxStore *TxStore
	TxErr   error
	TxFn    func(context.Context, func(repository.TxStore) error) error
}

func (s *Store) EnqueueTransfer(ctx context.Context, p domain.NewTransferParams) (domain.Transfer, bool, error) {
	if s.EnqueueFn != nil {
		return s.EnqueueFn(ctx, p)
	}
	return domain.Transfer{}, false, nil
}

func (s *Store) GetTransferByID(ctx context.Context, id uuid.UUID) (domain.Transfer, error) {
	if s.GetFn != nil {
		return s.GetFn(ctx, id)
	}
	return domain.Transfer{}, nil
}

func (s *Store) ClaimPending(ctx context.Context, workerID string, limit int) ([]domain.Transfer, error) {
	if s.ClaimFn != nil {
		return s.ClaimFn(ctx, workerID, limit)
	}
	return nil, nil
}

func (s *Store) ReapStuck(ctx context.Context, olderThan time.Duration) (int64, error) {
	if s.ReapFn != nil {
		return s.ReapFn(ctx, olderThan)
	}
	return 0, nil
}

func (s *Store) Tx(ctx context.Context, fn func(repository.TxStore) error) error {
	if s.TxFn != nil {
		return s.TxFn(ctx, fn)
	}
	tx := s.TxStore
	if tx == nil {
		tx = &TxStore{}
	}
	if err := fn(tx); err != nil {
		return err
	}
	return s.TxErr
}

// TxStore is a fake repository.TxStore that records terminal-state calls and
// allows per-method hooks.
type TxStore struct {
	GetFn    func(uuid.UUID) (domain.Transfer, error)
	LockFn   func(uuid.UUID, uuid.UUID) (domain.Wallet, domain.Wallet, error)
	ApplyFn  func(uuid.UUID, decimal.Decimal) error
	InsertFn func(domain.LedgerEntry) error

	SetProcessedErr error
	SetFailedErr    error

	ProcessedIDs  []uuid.UUID
	FailedReasons []string
	Deltas        []decimal.Decimal
	Ledger        []domain.LedgerEntry
}

func (t *TxStore) GetTransferForUpdate(_ context.Context, id uuid.UUID) (domain.Transfer, error) {
	if t.GetFn != nil {
		return t.GetFn(id)
	}
	return domain.Transfer{}, nil
}

func (t *TxStore) LockWalletPair(_ context.Context, from, to uuid.UUID) (domain.Wallet, domain.Wallet, error) {
	if t.LockFn != nil {
		return t.LockFn(from, to)
	}
	return domain.Wallet{}, domain.Wallet{}, nil
}

func (t *TxStore) ApplyBalanceDelta(_ context.Context, id uuid.UUID, delta decimal.Decimal) error {
	t.Deltas = append(t.Deltas, delta)
	if t.ApplyFn != nil {
		return t.ApplyFn(id, delta)
	}
	return nil
}

func (t *TxStore) InsertLedgerEntry(_ context.Context, e domain.LedgerEntry) error {
	t.Ledger = append(t.Ledger, e)
	if t.InsertFn != nil {
		return t.InsertFn(e)
	}
	return nil
}

func (t *TxStore) SetProcessed(_ context.Context, id uuid.UUID) error {
	t.ProcessedIDs = append(t.ProcessedIDs, id)
	return t.SetProcessedErr
}

func (t *TxStore) SetFailed(_ context.Context, _ uuid.UUID, reason string) error {
	t.FailedReasons = append(t.FailedReasons, reason)
	return t.SetFailedErr
}
