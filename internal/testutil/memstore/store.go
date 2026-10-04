// Package memstore is an in-memory port used by unit tests.
// A transaction holds one mutex for its whole body, so this store checks
// use-case branching and rollback. It does not model database row locks.
// Concurrency safety is tested against PostgreSQL.
package memstore

import (
	"context"
	"maps"
	"math"
	"sync"
	"time"

	"github.com/amantiwari/wallet-transfer/internal/app/port"
	"github.com/amantiwari/wallet-transfer/internal/domain"
)

// Store implements the application ports in process memory.
type Store struct {
	mu        sync.Mutex
	inTx      bool
	wallets   map[string]domain.Wallet
	transfers map[string]domain.Transfer
	entries   map[string][]domain.LedgerEntry
	idem      map[string]port.IdempotencyRecord
}

// New returns an empty store.
func New() *Store {
	return &Store{
		wallets:   map[string]domain.Wallet{},
		transfers: map[string]domain.Transfer{},
		entries:   map[string][]domain.LedgerEntry{},
		idem:      map[string]port.IdempotencyRecord{},
	}
}

type snapshot struct {
	wallets   map[string]domain.Wallet
	transfers map[string]domain.Transfer
	entries   map[string][]domain.LedgerEntry
	idem      map[string]port.IdempotencyRecord
}

// WithinTransaction runs fn. An error restores the pre-transaction snapshot.
func (s *Store) WithinTransaction(ctx context.Context, fn func(ctx context.Context, repos port.Repositories) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := s.clone()
	s.inTx = true
	defer func() { s.inTx = false }()
	if err := fn(ctx, txRepos{store: s}); err != nil {
		s.restore(snap)
		return err
	}
	return nil
}

func (s *Store) clone() snapshot {
	entries := make(map[string][]domain.LedgerEntry, len(s.entries))
	for id, rows := range s.entries {
		entries[id] = append([]domain.LedgerEntry(nil), rows...)
	}
	return snapshot{
		wallets:   maps.Clone(s.wallets),
		transfers: maps.Clone(s.transfers),
		entries:   entries,
		idem:      maps.Clone(s.idem),
	}
}

func (s *Store) restore(snap snapshot) {
	s.wallets = snap.wallets
	s.transfers = snap.transfers
	s.entries = snap.entries
	s.idem = snap.idem
}

func (s *Store) lock() {
	if !s.inTx {
		s.mu.Lock()
	}
}

func (s *Store) unlock() {
	if !s.inTx {
		s.mu.Unlock()
	}
}

// GetByID implements port.WalletReader.
func (s *Store) GetByID(ctx context.Context, id string) (domain.Wallet, error) {
	s.lock()
	defer s.unlock()
	wallet, ok := s.wallets[id]
	if !ok {
		return domain.Wallet{}, &domain.NotFoundError{Entity: "wallet", ID: id}
	}
	return wallet, nil
}

// TransferReader returns the transfer read port.
func (s *Store) TransferReader() port.TransferReader {
	return transferReader{store: s}
}

type transferReader struct{ store *Store }

func (r transferReader) GetByID(ctx context.Context, id string) (domain.Transfer, error) {
	r.store.lock()
	defer r.store.unlock()
	transfer, ok := r.store.transfers[id]
	if !ok {
		return domain.Transfer{}, &domain.NotFoundError{Entity: "transfer", ID: id}
	}
	return transfer, nil
}

// ListByTransferID implements port.LedgerReader.
func (s *Store) ListByTransferID(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	s.lock()
	defer s.unlock()
	return append([]domain.LedgerEntry(nil), s.entries[transferID]...), nil
}

type txRepos struct{ store *Store }

func (t txRepos) Wallets() port.WalletRepository          { return walletTx(t) }
func (t txRepos) Transfers() port.TransferRepository      { return transferTx(t) }
func (t txRepos) Ledger() port.LedgerRepository           { return ledgerTx(t) }
func (t txRepos) Idempotency() port.IdempotencyRepository { return idemTx(t) }

type walletTx struct{ store *Store }

func (w walletTx) Create(ctx context.Context, wallet domain.Wallet) error {
	if _, ok := w.store.wallets[wallet.ID]; ok {
		return &domain.ConflictError{Entity: "wallet", ID: wallet.ID}
	}
	w.store.wallets[wallet.ID] = wallet
	return nil
}

func (w walletTx) Lock(ctx context.Context, id string) (domain.Wallet, error) {
	wallet, ok := w.store.wallets[id]
	if !ok {
		return domain.Wallet{}, &domain.NotFoundError{Entity: "wallet", ID: id}
	}
	return wallet, nil
}

func (w walletTx) Debit(ctx context.Context, id string, amount int64, at time.Time) error {
	wallet, ok := w.store.wallets[id]
	if !ok {
		return &domain.NotFoundError{Entity: "wallet", ID: id}
	}
	if wallet.Balance < amount {
		return domain.ErrInsufficientFunds
	}
	wallet.Balance -= amount
	wallet.UpdatedAt = at.UTC()
	w.store.wallets[id] = wallet
	return nil
}

func (w walletTx) Credit(ctx context.Context, id string, amount int64, at time.Time) error {
	wallet, ok := w.store.wallets[id]
	if !ok {
		return &domain.NotFoundError{Entity: "wallet", ID: id}
	}
	if amount > 0 && wallet.Balance > math.MaxInt64-amount {
		return domain.ErrAmountOverflow
	}
	wallet.Balance += amount
	wallet.UpdatedAt = at.UTC()
	w.store.wallets[id] = wallet
	return nil
}

type transferTx struct{ store *Store }

func (t transferTx) Insert(ctx context.Context, transfer domain.Transfer) error {
	if _, ok := t.store.transfers[transfer.ID]; ok {
		return &domain.ConflictError{Entity: "transfer", ID: transfer.ID}
	}
	for _, existing := range t.store.transfers {
		if existing.IdempotencyKey == transfer.IdempotencyKey {
			return &domain.ConflictError{Entity: "transfer", ID: transfer.ID}
		}
	}
	t.store.transfers[transfer.ID] = transfer
	return nil
}

func (t transferTx) GetByID(ctx context.Context, id string) (domain.Transfer, error) {
	transfer, ok := t.store.transfers[id]
	if !ok {
		return domain.Transfer{}, &domain.NotFoundError{Entity: "transfer", ID: id}
	}
	return transfer, nil
}

func (t transferTx) Transition(ctx context.Context, updated domain.Transfer, from domain.Status) error {
	current, ok := t.store.transfers[updated.ID]
	if !ok {
		return &domain.NotFoundError{Entity: "transfer", ID: updated.ID}
	}
	if current.Status != from {
		return domain.ErrInvalidTransition
	}
	t.store.transfers[updated.ID] = updated
	return nil
}

type ledgerTx struct{ store *Store }

func (l ledgerTx) InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	if debit.TransferID != credit.TransferID || debit.Amount != credit.Amount {
		return domain.ErrInvariant
	}
	if debit.Type != domain.EntryDebit || credit.Type != domain.EntryCredit {
		return domain.ErrInvariant
	}
	l.store.entries[debit.TransferID] = []domain.LedgerEntry{debit, credit}
	return nil
}

type idemTx struct{ store *Store }

func (i idemTx) TryInsert(ctx context.Context, record port.IdempotencyRecord) (port.IdempotencyRecord, bool, error) {
	if existing, ok := i.store.idem[record.Key]; ok {
		return existing, false, nil
	}
	i.store.idem[record.Key] = record
	return record, true, nil
}

func (i idemTx) AttachTransfer(ctx context.Context, key, transferID string, at time.Time) error {
	record, ok := i.store.idem[key]
	if !ok {
		return &domain.NotFoundError{Entity: "idempotency key", ID: key}
	}
	if record.TransferID != "" {
		return domain.ErrConflict
	}
	record.TransferID = transferID
	record.UpdatedAt = at.UTC()
	i.store.idem[key] = record
	return nil
}
