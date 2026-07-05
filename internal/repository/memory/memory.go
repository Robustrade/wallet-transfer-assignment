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
// Rollback semantics: BeginTx snapshots the store's maps before handing out
// the Tx. Rollback restores those snapshots; Commit just discards them. This
// matters because service.CreateTransfer relies on real rollback behavior
// for unexpected failures — in particular, releasing an in-progress
// idempotency claim so a retry isn't stuck forever. Snapshot/restore is only
// correct because the mutex guarantees exactly one transaction is ever
// in-flight at a time; it would not be safe under real concurrent writers.
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
	return &tx{
		store:               s,
		walletsSnapshot:     cloneWallets(s.wallets),
		transfersSnapshot:   cloneTransfers(s.transfers),
		idempotencySnapshot: cloneIdempotency(s.idempotency),
		ledgerLen:           len(s.ledger),
	}, nil
}

type tx struct {
	store *Store
	done  bool

	// Snapshots taken at BeginTx, restored on Rollback so an in-flight
	// transaction's writes (including a claimed-but-not-yet-completed
	// idempotency record) are fully undone on an unexpected failure —
	// mirroring what a real SQL ROLLBACK gives you for free.
	walletsSnapshot     map[string]domain.Wallet
	transfersSnapshot   map[string]domain.Transfer
	idempotencySnapshot map[string]repository.IdempotencyRecord
	ledgerLen           int
}

func cloneWallets(m map[string]domain.Wallet) map[string]domain.Wallet {
	out := make(map[string]domain.Wallet, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneTransfers(m map[string]domain.Transfer) map[string]domain.Transfer {
	out := make(map[string]domain.Transfer, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneIdempotency(m map[string]repository.IdempotencyRecord) map[string]repository.IdempotencyRecord {
	out := make(map[string]repository.IdempotencyRecord, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
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
	if !t.done {
		// Restore pre-transaction state, including releasing any
		// idempotency claim this transaction made — this is what lets a
		// retry after an unexpected failure get a fresh attempt instead of
		// returning ErrDuplicateInFlight forever.
		t.store.wallets = t.walletsSnapshot
		t.store.transfers = t.transfersSnapshot
		t.store.idempotency = t.idempotencySnapshot
		t.store.ledger = t.store.ledger[:t.ledgerLen]
	}
	t.finish()
	return nil
}
