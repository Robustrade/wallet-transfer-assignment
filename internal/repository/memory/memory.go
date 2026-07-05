// Package memory provides an in-memory implementation of repository.TxStore.
// It exists purely for fast, dependency-free unit tests — it lets the
// service layer's business logic (idempotency, validation, the debit/credit
// workflow) be tested without spinning up Postgres.
//
// Concurrency model: a single mutex stands in for "the database transaction
// lock". BeginTx acquires it and it's released on Commit/Rollback, so at
// most one transaction runs at a time — the same end-to-end serializability
// a real "SELECT ... FOR UPDATE" transaction gives you against Postgres,
// just achieved with a coarser lock since there's no real row-level locking
// to model in memory.
//
// Known limitation: unlike a real SQL transaction, writes made by an
// in-flight tx are NOT rolled back on error (there's no undo log). This is
// safe for this codebase today because InsertTransfer is the only write
// before the wallet locks are acquired, and every other write in
// service.executeTransfer happens only after all prior steps succeeded — but
// it's a corner a real database gets for free and this fake does not.
package memory

import (
	"context"
	"sync"

	"github.com/example/wallet-transfer/internal/domain"
	"github.com/example/wallet-transfer/internal/repository"
)

type Store struct {
	mu sync.Mutex

	wallets     map[string]domain.Wallet
	transfers   map[string]domain.Transfer
	ledger      []domain.LedgerEntry
	idempotency map[string]repository.IdempotencyRecord
}

func NewStore() *Store {
	return &Store{
		wallets:     map[string]domain.Wallet{},
		transfers:   map[string]domain.Transfer{},
		idempotency: map[string]repository.IdempotencyRecord{},
	}
}

// SeedWallet is a test helper for setting up initial balances.
func (s *Store) SeedWallet(w domain.Wallet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wallets[w.ID] = w
}

// GetWallet is a test helper for asserting on final balances.
func (s *Store) GetWallet(id string) (domain.Wallet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.wallets[id]
	return w, ok
}

// Ledger is a test helper returning a snapshot of every posted entry.
func (s *Store) Ledger() []domain.LedgerEntry {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]domain.LedgerEntry, len(s.ledger))
	copy(out, s.ledger)
	return out
}

func (s *Store) BeginTx(ctx context.Context) (repository.Tx, error) {
	s.mu.Lock() // released in tx.finish(), called from Commit or Rollback
	return &tx{store: s}, nil
}

type tx struct {
	store *Store
	done  bool
}

func (t *tx) finish() {
	if !t.done {
		t.done = true
		t.store.mu.Unlock()
	}
}

func (t *tx) GetWalletForUpdate(ctx context.Context, walletID string) (domain.Wallet, error) {
	w, ok := t.store.wallets[walletID]
	if !ok {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return w, nil
}

func (t *tx) UpdateWalletBalance(ctx context.Context, walletID string, newBalance int64) error {
	w, ok := t.store.wallets[walletID]
	if !ok {
		return domain.ErrWalletNotFound
	}
	w.Balance = newBalance
	w.Version++
	t.store.wallets[walletID] = w
	return nil
}

func (t *tx) InsertTransfer(ctx context.Context, tr domain.Transfer) error {
	t.store.transfers[tr.ID] = tr
	return nil
}

func (t *tx) UpdateTransferState(ctx context.Context, transferID string, state domain.TransferState, failureReason string) error {
	tr, ok := t.store.transfers[transferID]
	if !ok {
		return domain.ErrWalletNotFound
	}
	if !tr.State.CanTransitionTo(state) {
		return domain.ErrInvalidStateTransition
	}
	tr.State = state
	tr.FailureReason = failureReason
	t.store.transfers[transferID] = tr
	return nil
}

func (t *tx) InsertLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	t.store.ledger = append(t.store.ledger, entries...)
	return nil
}

func (t *tx) ClaimIdempotencyKey(ctx context.Context, key, requestHash string) (bool, error) {
	if _, exists := t.store.idempotency[key]; exists {
		return false, nil
	}
	t.store.idempotency[key] = repository.IdempotencyRecord{
		Key:         key,
		RequestHash: requestHash,
		Status:      repository.IdempotencyInProgress,
	}
	return true, nil
}

func (t *tx) GetIdempotencyRecord(ctx context.Context, key string) (repository.IdempotencyRecord, error) {
	rec, ok := t.store.idempotency[key]
	if !ok {
		// Shouldn't happen in practice: this is only called right after a
		// failed claim, which means a record must already exist.
		return repository.IdempotencyRecord{}, domain.ErrWalletNotFound
	}
	return rec, nil
}

func (t *tx) CompleteIdempotencyKey(ctx context.Context, key string, status repository.IdempotencyStatus, transferID string, responseBody []byte) error {
	rec, ok := t.store.idempotency[key]
	if !ok {
		return domain.ErrWalletNotFound
	}
	rec.Status = status
	rec.TransferID = transferID
	rec.ResponseBody = responseBody
	t.store.idempotency[key] = rec
	return nil
}

func (t *tx) Commit() error {
	t.finish()
	return nil
}

func (t *tx) Rollback() error {
	t.finish()
	return nil
}
