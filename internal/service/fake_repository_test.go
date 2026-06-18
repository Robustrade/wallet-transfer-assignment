package service

import (
	"context"
	"sync"

	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/domain"
	"github.com/GowthamMuddusetty/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
)

// fakeStore is an in-memory TxManager + Repository for tests.
// A single mutex serializes WithTx calls, emulating transaction isolation.
type fakeStore struct {
	mu          sync.Mutex
	wallets     map[string]*domain.Wallet
	transfers   map[string]*domain.Transfer
	ledger      []*domain.LedgerEntry
	idempotency map[string]string // key -> transferID
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		wallets:     make(map[string]*domain.Wallet),
		transfers:   make(map[string]*domain.Transfer),
		idempotency: make(map[string]string),
	}
}

func (f *fakeStore) seedWallet(id string, balance int64) {
	f.wallets[id] = &domain.Wallet{ID: id, Balance: balance}
}

// WithTx locks the store for the duration of fn, emulating a serial tx.
func (f *fakeStore) WithTx(ctx context.Context, fn func(repo repository.Repository) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fn(f)
}

func (f *fakeStore) GetWalletForUpdate(ctx context.Context, walletID string) (*domain.Wallet, error) {
	w, ok := f.wallets[walletID]
	if !ok {
		return nil, repository.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (f *fakeStore) UpdateWalletBalance(ctx context.Context, walletID string, newBalance int64) error {
	w, ok := f.wallets[walletID]
	if !ok {
		return repository.ErrWalletNotFound
	}
	w.Balance = newBalance
	return nil
}

func (f *fakeStore) CreateTransfer(ctx context.Context, t *domain.Transfer) (*domain.Transfer, error) {
	cp := *t
	cp.ID = uuid.NewString()
	f.transfers[cp.ID] = &cp
	out := cp
	return &out, nil
}

func (f *fakeStore) UpdateTransferStatus(ctx context.Context, transferID string, status domain.TransferStatus) error {
	t, ok := f.transfers[transferID]
	if !ok {
		return repository.ErrTransferNotFound
	}
	t.Status = status
	return nil
}

func (f *fakeStore) GetTransferByID(ctx context.Context, transferID string) (*domain.Transfer, error) {
	t, ok := f.transfers[transferID]
	if !ok {
		return nil, repository.ErrTransferNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeStore) CreateLedgerEntry(ctx context.Context, e *domain.LedgerEntry) error {
	cp := *e
	f.ledger = append(f.ledger, &cp)
	return nil
}

func (f *fakeStore) CreateIdempotencyRecord(ctx context.Context, key string, transferID string) error {
	if _, exists := f.idempotency[key]; exists {
		return repository.ErrDuplicateIdempotencyKey
	}
	f.idempotency[key] = transferID
	return nil
}

func (f *fakeStore) GetTransferIDByIdempotencyKey(ctx context.Context, key string) (string, error) {
	id, ok := f.idempotency[key]
	if !ok {
		return "", repository.ErrIdempotencyKeyNotFound
	}
	return id, nil
}
