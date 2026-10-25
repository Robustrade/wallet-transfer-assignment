package memory

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"

	"github.com/safeek-dev/wallet-transfer-assignment/internal/model"
	"github.com/safeek-dev/wallet-transfer-assignment/internal/repository"
)

// MemoryRepository is an in-memory, thread-safe repository that implements repository.Repository.
// It supports sorted deterministic locking to verify concurrency and deadlock-free execution in unit/integration tests.
type MemoryRepository struct {
	txMu         sync.Mutex
	mu           sync.RWMutex
	wallets      map[string]*model.Wallet
	transfers    map[string]*model.Transfer
	transfersKey map[string]string // idempotency_key -> transfer_id
	ledger       map[string][]model.LedgerEntry
	idempotency  map[string]*model.IdempotencyRecord
}

var _ repository.Repository = (*MemoryRepository)(nil)

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		wallets:      make(map[string]*model.Wallet),
		transfers:    make(map[string]*model.Transfer),
		transfersKey: make(map[string]string),
		ledger:       make(map[string][]model.LedgerEntry),
		idempotency:  make(map[string]*model.IdempotencyRecord),
	}
}

func (m *MemoryRepository) Close() error {
	return nil
}

func (m *MemoryRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return nil, nil
}

func (m *MemoryRepository) ExecTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	m.txMu.Lock()
	defer m.txMu.Unlock()
	return fn(nil)
}

func (m *MemoryRepository) CreateWallet(ctx context.Context, w *model.Wallet) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.wallets[w.ID]; exists {
		return fmt.Errorf("wallet already exists: %s", w.ID)
	}

	clone := *w
	now := time.Now().UTC()
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = now
	}
	clone.UpdatedAt = now
	m.wallets[w.ID] = &clone
	return nil
}

func (m *MemoryRepository) GetWallet(ctx context.Context, id string) (*model.Wallet, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	w, exists := m.wallets[id]
	if !exists {
		return nil, model.ErrWalletNotFound
	}
	clone := *w
	return &clone, nil
}

// GetWalletsForUpdate retrieves both wallets while validating the deterministic sorted order.
func (m *MemoryRepository) GetWalletsForUpdate(ctx context.Context, tx *sql.Tx, id1, id2 string) (*model.Wallet, *model.Wallet, error) {
	if id1 == id2 {
		return nil, nil, model.ErrSelfTransfer
	}

	firstID, secondID := id1, id2
	swapped := false
	if firstID > secondID {
		firstID, secondID = id2, id1
		swapped = true
	}

	m.mu.RLock()
	w1, ok1 := m.wallets[firstID]
	w2, ok2 := m.wallets[secondID]
	m.mu.RUnlock()

	if !ok1 || !ok2 {
		return nil, nil, model.ErrWalletNotFound
	}

	clone1 := *w1
	clone2 := *w2

	if swapped {
		return &clone2, &clone1, nil
	}
	return &clone1, &clone2, nil
}

func (m *MemoryRepository) UpdateWalletBalance(ctx context.Context, tx *sql.Tx, id string, newBalance int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	w, exists := m.wallets[id]
	if !exists {
		return model.ErrWalletNotFound
	}
	if newBalance < 0 {
		return model.ErrInsufficientFunds
	}
	w.Balance = newBalance
	w.UpdatedAt = time.Now().UTC()
	return nil
}

func (m *MemoryRepository) CreateTransfer(ctx context.Context, tx *sql.Tx, t *model.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if _, exists := m.transfersKey[t.IdempotencyKey]; exists {
		return fmt.Errorf("duplicate idempotency key in transfers: %w", model.ErrIdempotencyConflict)
	}

	clone := *t
	now := time.Now().UTC()
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = now
	}
	clone.UpdatedAt = now

	m.transfers[t.ID] = &clone
	m.transfersKey[t.IdempotencyKey] = t.ID
	return nil
}

func (m *MemoryRepository) GetTransfer(ctx context.Context, id string) (*model.Transfer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	t, exists := m.transfers[id]
	if !exists {
		return nil, model.ErrTransferNotFound
	}
	clone := *t
	return &clone, nil
}

func (m *MemoryRepository) GetTransferByIdempotencyKey(ctx context.Context, key string) (*model.Transfer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	id, exists := m.transfersKey[key]
	if !exists {
		return nil, model.ErrTransferNotFound
	}
	t := m.transfers[id]
	clone := *t
	return &clone, nil
}

func (m *MemoryRepository) CreateLedgerEntries(ctx context.Context, tx *sql.Tx, entries []model.LedgerEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	for _, e := range entries {
		clone := e
		if clone.CreatedAt.IsZero() {
			clone.CreatedAt = now
		}
		m.ledger[clone.WalletID] = append(m.ledger[clone.WalletID], clone)
	}
	return nil
}

func (m *MemoryRepository) GetLedgerEntriesByTransferID(ctx context.Context, transferID string) ([]model.LedgerEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []model.LedgerEntry
	for _, entries := range m.ledger {
		for _, e := range entries {
			if e.TransferID == transferID {
				result = append(result, e)
			}
		}
	}
	return result, nil
}

func (m *MemoryRepository) GetLedgerEntriesByWalletID(ctx context.Context, walletID string) ([]model.LedgerEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, ok := m.ledger[walletID]
	if !ok {
		return []model.LedgerEntry{}, nil
	}
	result := make([]model.LedgerEntry, len(entries))
	copy(result, entries)
	return result, nil
}

func (m *MemoryRepository) GetLedgerBalance(ctx context.Context, walletID string) (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	entries, ok := m.ledger[walletID]
	if !ok {
		return 0, nil
	}
	var balance int64
	for _, e := range entries {
		if e.EntryType == model.EntryTypeCredit {
			balance += e.Amount
		} else if e.EntryType == model.EntryTypeDebit {
			balance -= e.Amount
		}
	}
	return balance, nil
}

func (m *MemoryRepository) GetIdempotencyRecord(ctx context.Context, key string) (*model.IdempotencyRecord, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	rec, exists := m.idempotency[key]
	if !exists {
		return nil, nil
	}
	clone := *rec
	return &clone, nil
}

func (m *MemoryRepository) SaveIdempotencyRecord(ctx context.Context, tx *sql.Tx, rec *model.IdempotencyRecord) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	clone := *rec
	now := time.Now().UTC()
	if clone.CreatedAt.IsZero() {
		clone.CreatedAt = now
	}
	clone.UpdatedAt = now
	m.idempotency[rec.Key] = &clone
	return nil
}
