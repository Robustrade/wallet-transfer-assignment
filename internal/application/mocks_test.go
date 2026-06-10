package application

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	repository "github.com/Robustrade/wallet-transfer-assignment/internal/respository"
)

// mockTransactionManager is a mock implementation of repository.TransactionManager.
type mockTransactionManager struct {
	RunInTransactionFn func(ctx context.Context, fn func(ctx context.Context) error) error
}

func (m *mockTransactionManager) RunInTransaction(ctx context.Context, fn func(ctx context.Context) error) error {
	if m.RunInTransactionFn != nil {
		return m.RunInTransactionFn(ctx, fn)
	}
	return fn(ctx)
}

// mockTransferRepo is a mock implementation of repository.TransferRepository.
type mockTransferRepo struct {
	mu                    sync.Mutex
	transfers             map[string]*domain.Transfer
	getByIDFn             func(ctx context.Context, id string) (*domain.Transfer, error)
	getByIdempotencyKeyFn func(ctx context.Context, key string) (*domain.Transfer, error)
}

func (m *mockTransferRepo) Create(ctx context.Context, transfer *domain.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.transfers == nil {
		m.transfers = make(map[string]*domain.Transfer)
	}
	m.transfers[transfer.ID] = transfer
	return nil
}
func (m *mockTransferRepo) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	if m.getByIDFn != nil {
		return m.getByIDFn(ctx, id)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if t, ok := m.transfers[id]; ok {
		return t, nil
	}
	return nil, repository.ErrNotFound
}
func (m *mockTransferRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	if m.getByIdempotencyKeyFn != nil {
		return m.getByIdempotencyKeyFn(ctx, key)
	}
	return nil, repository.ErrNotFound
}
func (m *mockTransferRepo) UpdateStatus(ctx context.Context, id string, status domain.TransferStatus, reason string) error {
	return nil
}

// mockIdempotencyRepo is a mock implementation of repository.IdempotencyRepository.
type mockIdempotencyRepo struct {
	mu         sync.Mutex
	records    map[string]*repository.IdempotencyRecord
	getByKeyFn func(ctx context.Context, key string) (*repository.IdempotencyRecord, error)
}

func (m *mockIdempotencyRepo) Create(ctx context.Context, record *repository.IdempotencyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.records == nil {
		m.records = make(map[string]*repository.IdempotencyRecord)
	}
	m.records[record.IdempotencyKey] = record
	return nil
}
func (m *mockIdempotencyRepo) GetByKey(ctx context.Context, idempotencyKey string) (*repository.IdempotencyRecord, error) {
	if m.getByKeyFn != nil {
		return m.getByKeyFn(ctx, idempotencyKey)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.records[idempotencyKey]; ok {
		return r, nil
	}
	return nil, repository.ErrNotFound
}
func (m *mockIdempotencyRepo) UpdateResponse(ctx context.Context, idempotencyKey string, responseJSON []byte, statusCode int, status repository.IdempotencyStatus) error {
	return nil
}
func (m *mockIdempotencyRepo) UpdateStatus(ctx context.Context, idempotencyKey string, status repository.IdempotencyStatus) error {
	return nil
}

func (m *mockIdempotencyRepo) CleanupPending(ctx context.Context, cutoff time.Time) (int, error) {
	return 0, nil
}

// mockLedgerRepoForTest is a mock implementation of repository.LedgerRepository.
type mockLedgerRepoForTest struct{}

func (m *mockLedgerRepoForTest) CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	if err := domain.ValidateLedgerEntries(entries); err != nil {
		return err
	}
	// In a real mock, you might store these for later verification
	return nil
}
func (m *mockLedgerRepoForTest) GetByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return nil, nil
}
func (m *mockLedgerRepoForTest) GetByWalletID(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	return nil, nil
}

// mockWalletRepoWithLocking is a mock implementation of repository.WalletRepository that simulates locking.
type mockWalletRepoWithLocking struct {
	mu      sync.Mutex
	wallets map[string]*domain.Wallet
	locks   map[string]*sync.Mutex
}

func (m *mockWalletRepoWithLocking) getLock(id string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.locks == nil {
		m.locks = make(map[string]*sync.Mutex)
	}
	l, ok := m.locks[id]
	if !ok {
		l = &sync.Mutex{}
		m.locks[id] = l
	}
	return l
}

func (m *mockWalletRepoWithLocking) Create(ctx context.Context, wallet *domain.Wallet) error {
	return nil
}
func (m *mockWalletRepoWithLocking) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	w, ok := m.wallets[id]
	if !ok {
		return nil, errors.New("not found")
	}
	return w, nil
}
func (m *mockWalletRepoWithLocking) GetByIDForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	m.getLock(id).Lock()
	return m.GetByID(ctx, id)
}
func (m *mockWalletRepoWithLocking) Update(ctx context.Context, wallet *domain.Wallet) error {
	m.mu.Lock()
	m.wallets[wallet.ID] = wallet
	m.mu.Unlock() // Unlock the map mutex
	m.getLock(wallet.ID).Unlock()
	return nil
}
