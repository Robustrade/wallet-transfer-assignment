package memory

import (
	"context"
	"sync"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
	"github.com/avinilcodes/wallet-transfer-assignment/internal/repository"
)

// Store is an in-memory repository.Store for unit tests.
type Store struct {
	mu          sync.Mutex
	inTx        bool
	wallets     map[string]*domain.Wallet
	transfers   map[string]*domain.Transfer
	ledger      []domain.LedgerEntry
	idempotency map[string]*domain.IdempotencyRecord
	nextEntryID int64
}

// NewStore returns an empty in-memory store.
func NewStore() *Store {
	return &Store{
		wallets:     make(map[string]*domain.Wallet),
		transfers:   make(map[string]*domain.Transfer),
		idempotency: make(map[string]*domain.IdempotencyRecord),
	}
}

// SeedWallet adds or replaces a wallet (test helper).
func (s *Store) SeedWallet(wallet *domain.Wallet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	copy := *wallet
	s.wallets[wallet.ID] = &copy
}

func (s *Store) Wallets() repository.WalletRepository     { return &walletRepo{s: s} }
func (s *Store) Transfers() repository.TransferRepository { return &transferRepo{s: s} }
func (s *Store) Ledger() repository.LedgerRepository      { return &ledgerRepo{s: s} }
func (s *Store) Idempotency() repository.IdempotencyRepository {
	return &idempotencyRepo{s: s}
}
func (s *Store) Tx() repository.TxManager { return &txManager{s: s} }

func (s *Store) mustBeInTransaction() {
	if !s.inTx {
		panic("memory store: operation requires active transaction")
	}
}

// LedgerEntries returns a copy of ledger rows (test helper).
func (s *Store) LedgerEntries() []domain.LedgerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.LedgerEntry, len(s.ledger))
	copy(out, s.ledger)
	return out
}

// Wallet returns a wallet snapshot (test helper).
func (s *Store) Wallet(id string) (*domain.Wallet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.wallets[id]
	if !ok {
		return nil, false
	}
	copy := *w
	return &copy, true
}

// Transfer returns a transfer snapshot (test helper).
func (s *Store) Transfer(id string) (*domain.Transfer, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.transfers[id]
	if !ok {
		return nil, false
	}
	copy := *t
	return &copy, true
}

type txManager struct {
	s *Store
}

func (m *txManager) WithTransaction(ctx context.Context, fn func(context.Context) error) error {
	m.s.mu.Lock()
	m.s.inTx = true
	defer func() {
		m.s.inTx = false
		m.s.mu.Unlock()
	}()
	return fn(ctx)
}
