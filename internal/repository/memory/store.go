// Package memory provides an in-memory implementation of the repository ports.
//
// It is used for fast, dependency-free tests (including concurrency tests under
// the race detector) and for local experimentation. The implementation models
// the two persistence guarantees the service relies on:
//
//   - row-level locking: WithinTx serialises transactions that touch an
//     overlapping set of wallets, mirroring SELECT ... FOR UPDATE in Postgres.
//   - a unique idempotency_key constraint: InsertIfAbsent only inserts once per
//     key.
//
// The locking model is intentionally coarse (a single store mutex plus
// per-transaction wallet locks) because correctness, not throughput, is the
// goal of the in-memory store.
package memory

import (
	"context"
	"sort"
	"sync"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
)

// Store is an in-memory, transactional implementation of repository.Store.
type Store struct {
	mu sync.Mutex // guards the maps below and the per-wallet lock table

	wallets   map[string]domain.Wallet
	transfers map[string]domain.Transfer
	keyToID   map[string]string // idempotency_key -> transfer id
	ledger    []domain.LedgerEntry
	nextEntry int64

	// walletLocks serialises transactions per wallet, emulating row locks.
	walletLocks map[string]*sync.Mutex

	// keyLocks serialises transactions per idempotency key, emulating the
	// blocking behaviour of a unique index: a concurrent insert of the same key
	// waits for the in-flight transaction to commit or roll back.
	keyLocks map[string]*sync.Mutex
}

// NewStore creates an empty in-memory store.
func NewStore() *Store {
	return &Store{
		wallets:     make(map[string]domain.Wallet),
		transfers:   make(map[string]domain.Transfer),
		keyToID:     make(map[string]string),
		walletLocks: make(map[string]*sync.Mutex),
		keyLocks:    make(map[string]*sync.Mutex),
		nextEntry:   1,
	}
}

// Close implements repository.Store. There is nothing to release in memory.
func (s *Store) Close() {}

// lockFor returns (creating if needed) the mutex guarding a wallet id.
func (s *Store) lockFor(id string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.walletLocks[id]
	if !ok {
		m = &sync.Mutex{}
		s.walletLocks[id] = m
	}
	return m
}

// keyLockFor returns (creating if needed) the mutex guarding an idempotency key.
func (s *Store) keyLockFor(key string) *sync.Mutex {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.keyLocks[key]
	if !ok {
		m = &sync.Mutex{}
		s.keyLocks[key] = m
	}
	return m
}

// tx is the unit-of-work handed to a WithinTx callback. It buffers mutations and
// records which wallet rows have been locked so they can be released on commit
// or rollback.
type tx struct {
	store     *Store
	heldLocks map[string]*sync.Mutex // wallet row locks
	heldKeys  map[string]*sync.Mutex // idempotency-key locks

	// staged changes, applied to the store only on commit.
	walletWrites   map[string]domain.Wallet
	transferWrites map[string]domain.Transfer
	keyWrites      map[string]string
	ledgerWrites   []domain.LedgerEntry
}

// WithinTx runs fn against staged repositories and commits on success.
//
// Wallet locks acquired via GetForUpdate are released when the function
// returns, so a transaction holds its row locks for its full duration just like
// a database transaction.
func (s *Store) WithinTx(ctx context.Context, fn func(ctx context.Context, repos repository.Repositories) error) error {
	t := &tx{
		store:          s,
		heldLocks:      make(map[string]*sync.Mutex),
		heldKeys:       make(map[string]*sync.Mutex),
		walletWrites:   make(map[string]domain.Wallet),
		transferWrites: make(map[string]domain.Transfer),
		keyWrites:      make(map[string]string),
	}
	defer t.releaseLocks()

	repos := repository.Repositories{
		Wallets:   &walletTxRepo{tx: t},
		Transfers: &transferTxRepo{tx: t},
		Ledger:    &ledgerTxRepo{tx: t},
	}
	if err := fn(ctx, repos); err != nil {
		return err // staged changes discarded -> rollback
	}
	t.commit()
	return nil
}

// releaseLocks unlocks every wallet row lock and idempotency-key lock held by
// the transaction, in a deterministic order for predictability.
func (t *tx) releaseLocks() {
	releaseSorted(t.heldLocks)
	releaseSorted(t.heldKeys)
}

// releaseSorted unlocks the mutexes in a map in sorted key order.
func releaseSorted(locks map[string]*sync.Mutex) {
	ids := make([]string, 0, len(locks))
	for id := range locks {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		locks[id].Unlock()
	}
}

// commit applies all staged writes to the shared store atomically.
func (t *tx) commit() {
	s := t.store
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, w := range t.walletWrites {
		s.wallets[id] = w
	}
	for id, tr := range t.transferWrites {
		s.transfers[id] = tr
	}
	for k, id := range t.keyWrites {
		s.keyToID[k] = id
	}
	for _, e := range t.ledgerWrites {
		e.ID = s.nextEntry
		s.nextEntry++
		s.ledger = append(s.ledger, e)
	}
}

// Reader returns repositories that read directly from committed state.
func (s *Store) Reader() repository.Repositories {
	return repository.Repositories{
		Wallets:   &walletReadRepo{store: s},
		Transfers: &transferReadRepo{store: s},
		Ledger:    &ledgerReadRepo{store: s},
	}
}
