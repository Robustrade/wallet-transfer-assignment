package memory

import (
	"context"
	"sync"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/repository"
)

type Store struct {
	mu         *sync.Mutex
	standalone bool

	wallets     map[string]*domain.Wallet
	transfers   map[string]*domain.Transfer
	ledger      map[string][]*domain.LedgerEntry
	idempotency map[string]*domain.IdempotencyRecord
	nextEntryID *int64
}

func NewStore() *Store {
	return &Store{
		mu:          &sync.Mutex{},
		standalone:  true,
		wallets:     make(map[string]*domain.Wallet),
		transfers:   make(map[string]*domain.Transfer),
		ledger:      make(map[string][]*domain.LedgerEntry),
		idempotency: make(map[string]*domain.IdempotencyRecord),
		nextEntryID: new(int64),
	}
}

func (s *Store) guard() func() {
	if s.standalone {
		s.mu.Lock()
		return s.mu.Unlock
	}
	return func() {}
}

func (s *Store) Wallets() repository.WalletRepository          { return (*walletRepo)(s) }
func (s *Store) Transfers() repository.TransferRepository      { return (*transferRepo)(s) }
func (s *Store) Ledger() repository.LedgerRepository           { return (*ledgerRepo)(s) }
func (s *Store) Idempotency() repository.IdempotencyRepository { return (*idempotencyRepo)(s) }

func (s *Store) ExecTx(ctx context.Context, fn func(s repository.Store) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	txStore := *s
	txStore.standalone = false
	return fn(&txStore)
}
