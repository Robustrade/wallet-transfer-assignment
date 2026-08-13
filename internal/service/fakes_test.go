package service

import (
	"context"
	"sync"

	"github.com/robustrade/wallet-transfer-assignment/internal/domain"
)

// In-memory fakes implementing the repository interfaces, used to unit test
// TransferService's business logic without a real database. They are
// intentionally simple: real transactional/locking guarantees are proven
// separately against a real Postgres in the integration tests.

// fakeTxManager runs fn directly (no real transaction). commitErr, when set,
// simulates an error surfacing from the commit step itself -- i.e. fn
// succeeded, but WithinTx still failed -- to exercise callers' handling of
// that case (see domain.ErrCommitOutcomeUnknown).
type fakeTxManager struct {
	commitErr error
}

func (f fakeTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	if err := fn(ctx); err != nil {
		return err
	}
	return f.commitErr
}

type fakeWalletRepo struct {
	mu      sync.Mutex
	wallets map[string]*domain.Wallet
}

func newFakeWalletRepo(seed ...*domain.Wallet) *fakeWalletRepo {
	r := &fakeWalletRepo{wallets: map[string]*domain.Wallet{}}
	for _, w := range seed {
		cp := *w
		r.wallets[w.ID] = &cp
	}
	return r
}

func (r *fakeWalletRepo) Create(ctx context.Context, w *domain.Wallet) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.wallets[w.ID]; exists {
		return domain.ErrWalletAlreadyExists
	}
	cp := *w
	r.wallets[w.ID] = &cp
	return nil
}

func (r *fakeWalletRepo) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (r *fakeWalletRepo) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	return r.Get(ctx, id)
}

func (r *fakeWalletRepo) UpdateBalance(ctx context.Context, id string, newBalance int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.wallets[id]
	if !ok {
		return domain.ErrWalletNotFound
	}
	w.Balance = newBalance
	return nil
}

type fakeTransferRepo struct {
	mu        sync.Mutex
	transfers map[string]*domain.Transfer
}

func newFakeTransferRepo() *fakeTransferRepo {
	return &fakeTransferRepo{transfers: map[string]*domain.Transfer{}}
}

func (r *fakeTransferRepo) Create(ctx context.Context, t *domain.Transfer) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *t
	r.transfers[t.ID] = &cp
	return nil
}

func (r *fakeTransferRepo) UpdateStatus(ctx context.Context, id string, status domain.TransferStatus, failureReason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.transfers[id]
	if !ok {
		return domain.ErrTransferNotFound
	}
	t.Status = status
	t.FailureReason = failureReason
	return nil
}

func (r *fakeTransferRepo) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	t, ok := r.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	cp := *t
	return &cp, nil
}

type fakeLedgerRepo struct {
	mu      sync.Mutex
	entries []domain.LedgerEntry
}

func newFakeLedgerRepo() *fakeLedgerRepo {
	return &fakeLedgerRepo{}
}

func (r *fakeLedgerRepo) InsertEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.entries = append(r.entries, entries...)
	return nil
}

func (r *fakeLedgerRepo) ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []domain.LedgerEntry
	for _, e := range r.entries {
		if e.WalletID == walletID {
			out = append(out, e)
		}
	}
	return out, nil
}

type fakeIdempotencyRepo struct {
	mu           sync.Mutex
	records      map[string]*domain.IdempotencyRecord
	releaseCalls int
}

func newFakeIdempotencyRepo() *fakeIdempotencyRepo {
	return &fakeIdempotencyRepo{records: map[string]*domain.IdempotencyRecord{}}
}

func (r *fakeIdempotencyRepo) Claim(ctx context.Context, key, requestHash string) (*domain.IdempotencyRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if existing, ok := r.records[key]; ok {
		cp := *existing
		return &cp, false, nil
	}
	rec := &domain.IdempotencyRecord{IdempotencyKey: key, RequestHash: requestHash, Status: domain.IdempotencyPending}
	r.records[key] = rec
	cp := *rec
	return &cp, true, nil
}

func (r *fakeIdempotencyRepo) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[key]
	if !ok {
		return nil, domain.ErrIdempotencyRecordNotFound
	}
	cp := *rec
	return &cp, nil
}

func (r *fakeIdempotencyRepo) Complete(ctx context.Context, key, transferID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.records[key]
	if !ok {
		return domain.ErrIdempotencyRecordNotFound
	}
	rec.Status = domain.IdempotencyCompleted
	rec.TransferID = transferID
	return nil
}

func (r *fakeIdempotencyRepo) Release(ctx context.Context, key string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.releaseCalls++
	if rec, ok := r.records[key]; ok && rec.Status == domain.IdempotencyPending {
		delete(r.records, key)
	}
	return nil
}
