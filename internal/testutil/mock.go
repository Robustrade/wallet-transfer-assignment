// Package testutil provides repository mocks for tests that do not require a
// live database.
package testutil

import (
	"context"
	"sync"
	"time"

	"github.com/wallet-transfer-assignment/wallet-transfer/internal/domain"
	"github.com/wallet-transfer-assignment/wallet-transfer/internal/repository"
)

type mockStore struct {
	wallets     map[string]domain.Wallet
	transfers   map[string]domain.Transfer
	ledger      []domain.LedgerEntry
	idempotency map[string]domain.IdempotencyRecord
}

type mockUnitOfWork struct {
	mu    sync.Mutex
	store *mockStore
}

// NewMockUnitOfWork returns a repository.UnitOfWork backed by an isolated
// in-process store. It is intended for service and HTTP tests only.
func NewMockUnitOfWork() repository.UnitOfWork {
	return &mockUnitOfWork{store: &mockStore{
		wallets:     make(map[string]domain.Wallet),
		transfers:   make(map[string]domain.Transfer),
		idempotency: make(map[string]domain.IdempotencyRecord),
	}}
}

func (u *mockUnitOfWork) Execute(ctx context.Context, fn func(context.Context, repository.Repositories) error) error {
	u.mu.Lock()
	defer u.mu.Unlock()

	snapshot := u.store.clone()
	repos := repository.Repositories{
		Wallets:     &mockWalletRepo{store: u.store},
		Transfers:   &mockTransferRepo{store: u.store},
		Ledger:      &mockLedgerRepo{store: u.store},
		Idempotency: &mockIdempotencyRepo{store: u.store},
	}
	if err := fn(ctx, repos); err != nil {
		u.store = snapshot
		return err
	}
	return nil
}

type mockWalletRepo struct{ store *mockStore }

func (r *mockWalletRepo) Create(_ context.Context, wallet *domain.Wallet) error {
	if _, exists := r.store.wallets[wallet.ID]; exists {
		return domain.ErrWalletAlreadyExists
	}
	r.store.wallets[wallet.ID] = *wallet
	return nil
}

func (r *mockWalletRepo) Get(_ context.Context, id string) (*domain.Wallet, error) {
	wallet, exists := r.store.wallets[id]
	if !exists {
		return nil, domain.WalletNotFoundError{ID: id}
	}
	return &wallet, nil
}

func (r *mockWalletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.Get(ctx, id)
}

func (r *mockWalletRepo) UpdateBalance(_ context.Context, id string, balance int64) error {
	wallet, exists := r.store.wallets[id]
	if !exists {
		return domain.WalletNotFoundError{ID: id}
	}
	wallet.Balance = balance
	wallet.UpdatedAt = time.Now()
	r.store.wallets[id] = wallet
	return nil
}

type mockTransferRepo struct{ store *mockStore }

func (r *mockTransferRepo) Create(_ context.Context, transfer *domain.Transfer) error {
	for _, existing := range r.store.transfers {
		if existing.IdempotencyKey == transfer.IdempotencyKey {
			return domain.ErrIdempotencyKeyConflict
		}
	}
	r.store.transfers[transfer.ID] = *transfer
	return nil
}

func (r *mockTransferRepo) UpdateState(_ context.Context, id string, state domain.TransferState, failureReason string) error {
	transfer, exists := r.store.transfers[id]
	if !exists {
		return domain.ErrTransferNotFound
	}
	transfer.State = state
	transfer.FailureReason = failureReason
	transfer.UpdatedAt = time.Now()
	r.store.transfers[id] = transfer
	return nil
}

func (r *mockTransferRepo) GetByID(_ context.Context, id string) (*domain.Transfer, error) {
	transfer, exists := r.store.transfers[id]
	if !exists {
		return nil, domain.ErrTransferNotFound
	}
	return &transfer, nil
}

func (r *mockTransferRepo) ListByWallet(_ context.Context, walletID string) ([]domain.Transfer, error) {
	transfers := make([]domain.Transfer, 0)
	for _, transfer := range r.store.transfers {
		if transfer.FromWalletID == walletID || transfer.ToWalletID == walletID {
			transfers = append(transfers, transfer)
		}
	}
	return transfers, nil
}

type mockLedgerRepo struct{ store *mockStore }

func (r *mockLedgerRepo) CreateEntries(_ context.Context, entries []domain.LedgerEntry) error {
	r.store.ledger = append(r.store.ledger, entries...)
	return nil
}

func (r *mockLedgerRepo) ListByTransfer(_ context.Context, transferID string) ([]domain.LedgerEntry, error) {
	entries := make([]domain.LedgerEntry, 0)
	for _, entry := range r.store.ledger {
		if entry.TransferID == transferID {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func (r *mockLedgerRepo) ListByWallet(_ context.Context, walletID string) ([]domain.LedgerEntry, error) {
	entries := make([]domain.LedgerEntry, 0)
	for _, entry := range r.store.ledger {
		if entry.WalletID == walletID {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

type mockIdempotencyRepo struct{ store *mockStore }

func (r *mockIdempotencyRepo) Claim(_ context.Context, key, requestHash string) (bool, *domain.IdempotencyRecord, error) {
	if record, exists := r.store.idempotency[key]; exists {
		return false, cloneIdempotencyRecord(&record), nil
	}
	record := domain.IdempotencyRecord{Key: key, RequestHash: requestHash, CreatedAt: time.Now()}
	r.store.idempotency[key] = record
	return true, nil, nil
}

func (r *mockIdempotencyRepo) Complete(_ context.Context, key, transferID string, responseStatus int, responseBody []byte) error {
	record, exists := r.store.idempotency[key]
	if !exists {
		return domain.ErrTransferNotFound
	}
	now := time.Now()
	record.TransferID = transferID
	record.ResponseStatus = responseStatus
	record.ResponseBody = append([]byte(nil), responseBody...)
	record.CompletedAt = &now
	r.store.idempotency[key] = record
	return nil
}

func (s *mockStore) clone() *mockStore {
	clone := &mockStore{
		wallets:     make(map[string]domain.Wallet, len(s.wallets)),
		transfers:   make(map[string]domain.Transfer, len(s.transfers)),
		ledger:      append([]domain.LedgerEntry(nil), s.ledger...),
		idempotency: make(map[string]domain.IdempotencyRecord, len(s.idempotency)),
	}
	for key, wallet := range s.wallets {
		clone.wallets[key] = wallet
	}
	for key, transfer := range s.transfers {
		clone.transfers[key] = transfer
	}
	for key, record := range s.idempotency {
		clone.idempotency[key] = *cloneIdempotencyRecord(&record)
	}
	return clone
}

func cloneIdempotencyRecord(record *domain.IdempotencyRecord) *domain.IdempotencyRecord {
	clone := *record
	clone.ResponseBody = append([]byte(nil), record.ResponseBody...)
	if record.CompletedAt != nil {
		completedAt := *record.CompletedAt
		clone.CompletedAt = &completedAt
	}
	return &clone
}
