// Package memory is an in-memory implementation of the service persistence
// ports. It is used for fast hermetic tests (no database required) and for a
// local "REPO=memory" run mode. A single mutex plus copy-on-write snapshots
// give it transaction semantics equivalent to SERIALIZABLE.
package memory

import (
	"context"
	"sync"
	"time"

	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/domain"
	"github.com/gaurav2204bits-collab/wallet-transfer-assignment/internal/service"
)

type txKey struct{}

func inTx(ctx context.Context) bool {
	v, _ := ctx.Value(txKey{}).(bool)
	return v
}

// Store holds all state and implements every service port plus
// service.TxManager.
type Store struct {
	mu           sync.Mutex
	wallets      map[string]domain.Wallet
	transfers    map[string]domain.Transfer
	transferByIK map[string]string // idempotency key -> transfer id
	idempotency  map[string]service.IdempotencyRecord
	ledger       []domain.LedgerEntry
}

// New returns an empty Store.
func New() *Store {
	return &Store{
		wallets:      make(map[string]domain.Wallet),
		transfers:    make(map[string]domain.Transfer),
		transferByIK: make(map[string]string),
		idempotency:  make(map[string]service.IdempotencyRecord),
	}
}

// SeedWallet inserts or replaces a wallet. For tests and local bootstrapping.
func (s *Store) SeedWallet(id string, balance int64, currency string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.wallets[id] = domain.Wallet{
		ID:      id,
		Balance: domain.Money{Amount: balance, Currency: currency},
	}
}

// lock acquires the mutex unless the caller is already inside WithinTx, which
// holds it for the duration of the callback.
func (s *Store) lock(ctx context.Context) func() {
	if inTx(ctx) {
		return func() { /* lock already held by WithinTx */ }
	}
	s.mu.Lock()
	return s.mu.Unlock
}

// WithinTx runs fn under the global lock. On error, all mutations fn made are
// rolled back from the pre-transaction snapshot.
func (s *Store) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap := s.snapshot()
	if err := fn(context.WithValue(ctx, txKey{}, true)); err != nil {
		s.restore(snap)
		return err
	}
	return nil
}

// --- WalletRepository ---

func (s *Store) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return s.Get(ctx, id)
}

func (s *Store) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	defer s.lock(ctx)()
	w, ok := s.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	return &w, nil
}

func (s *Store) UpdateBalance(ctx context.Context, w *domain.Wallet) error {
	defer s.lock(ctx)()
	stored, ok := s.wallets[w.ID]
	if !ok {
		return domain.ErrWalletNotFound
	}
	stored.Balance = w.Balance
	stored.Version++
	s.wallets[w.ID] = stored
	w.Version = stored.Version
	return nil
}

// --- TransferRepository ---

func (s *Store) Create(ctx context.Context, t *domain.Transfer) error {
	defer s.lock(ctx)()
	if _, exists := s.transferByIK[t.IdempotencyKey]; exists {
		// Mirrors the unique constraint on transfers.idempotency_key.
		return domain.ErrDuplicateIdempotencyKey
	}
	now := time.Now().UTC()
	t.CreatedAt, t.UpdatedAt = now, now
	cp := *t
	s.transfers[t.ID] = cp
	s.transferByIK[t.IdempotencyKey] = t.ID
	return nil
}

func (s *Store) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	defer s.lock(ctx)()
	t, ok := s.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	return &t, nil
}

func (s *Store) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	defer s.lock(ctx)()
	id, ok := s.transferByIK[key]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	t := s.transfers[id]
	return &t, nil
}

func (s *Store) UpdateState(ctx context.Context, t *domain.Transfer) error {
	defer s.lock(ctx)()
	stored, ok := s.transfers[t.ID]
	if !ok {
		return domain.ErrTransferNotFound
	}
	stored.State = t.State
	stored.FailureReason = t.FailureReason
	s.transfers[t.ID] = stored
	return nil
}

// --- IdempotencyRepository ---

func (s *Store) Insert(ctx context.Context, rec service.IdempotencyRecord) error {
	defer s.lock(ctx)()
	if _, exists := s.idempotency[rec.Key]; exists {
		return domain.ErrDuplicateIdempotencyKey
	}
	s.idempotency[rec.Key] = rec
	return nil
}

func (s *Store) GetRecord(ctx context.Context, key string) (service.IdempotencyRecord, error) {
	defer s.lock(ctx)()
	rec, ok := s.idempotency[key]
	if !ok {
		return service.IdempotencyRecord{}, domain.ErrTransferNotFound
	}
	return rec, nil
}

// --- LedgerRepository ---

func (s *Store) InsertPair(ctx context.Context, debit, credit domain.LedgerEntry) error {
	defer s.lock(ctx)()
	for _, e := range s.ledger {
		if e.TransferID == debit.TransferID {
			return domain.ErrValidation // already recorded — mimics unique (transfer_id, type)
		}
	}
	next := int64(len(s.ledger) + 1)
	debit.ID, credit.ID = next, next+1
	s.ledger = append(s.ledger, debit, credit)
	return nil
}

func (s *Store) SumByWallet(ctx context.Context, walletID string) (domain.Money, error) {
	defer s.lock(ctx)()
	sum := domain.Money{Currency: "USD"}
	for _, e := range s.ledger {
		if e.WalletID != walletID {
			continue
		}
		sum.Currency = e.Amount.Currency
		switch e.Type {
		case domain.EntryCredit:
			sum.Amount += e.Amount.Amount
		case domain.EntryDebit:
			sum.Amount -= e.Amount.Amount
		}
	}
	return sum, nil
}

// LedgerLen reports the number of ledger rows; test helper.
func (s *Store) LedgerLen() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.ledger)
}

// --- snapshot / restore ---

type snapshot struct {
	wallets      map[string]domain.Wallet
	transfers    map[string]domain.Transfer
	transferByIK map[string]string
	idempotency  map[string]service.IdempotencyRecord
	ledger       []domain.LedgerEntry
}

func (s *Store) snapshot() snapshot {
	return snapshot{
		wallets:      cloneMap(s.wallets),
		transfers:    cloneMap(s.transfers),
		transferByIK: cloneMap(s.transferByIK),
		idempotency:  cloneMap(s.idempotency),
		ledger:       append([]domain.LedgerEntry(nil), s.ledger...),
	}
}

func (s *Store) restore(snap snapshot) {
	s.wallets = snap.wallets
	s.transfers = snap.transfers
	s.transferByIK = snap.transferByIK
	s.idempotency = snap.idempotency
	s.ledger = snap.ledger
}

func cloneMap[K comparable, V any](m map[K]V) map[K]V {
	out := make(map[K]V, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
