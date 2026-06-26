package service

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/feroz-khatri/wallet-transfer-assignment/internal/domain"
)

// fakeTxManager runs fn under a process-wide lock. There is no real database
// transaction; serialising fn simulates the isolation a real SERIALIZABLE/
// row-locked transaction provides, so concurrent callers cannot interleave
// reads and writes (no lost updates). The fakes still simulate the durable
// unique-constraint behaviour on their own.
type fakeTxManager struct {
	mu *sync.Mutex
}

func newFakeTxManager() fakeTxManager {
	return fakeTxManager{mu: &sync.Mutex{}}
}

func (f fakeTxManager) WithinTx(ctx context.Context, fn func(ctx context.Context) error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	return fn(ctx)
}

// fakeWallets is a thread-safe in-memory WalletRepository.
type fakeWallets struct {
	mu      sync.Mutex
	wallets map[string]*domain.Wallet
}

func newFakeWallets() *fakeWallets {
	return &fakeWallets{wallets: make(map[string]*domain.Wallet)}
}

func (f *fakeWallets) seed(id string, balance domain.Money) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now().UTC()
	f.wallets[id] = &domain.Wallet{ID: id, Balance: balance, CreatedAt: now, UpdatedAt: now}
}

func (f *fakeWallets) Create(ctx context.Context, wallet *domain.Wallet) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := *wallet
	f.wallets[wallet.ID] = &cp
	return nil
}

func (f *fakeWallets) get(id string) (*domain.Wallet, error) {
	w, ok := f.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	cp := *w
	return &cp, nil
}

func (f *fakeWallets) Get(ctx context.Context, id string) (*domain.Wallet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.get(id)
}

func (f *fakeWallets) GetForUpdate(ctx context.Context, id string) (*domain.Wallet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.get(id)
}

func (f *fakeWallets) UpdateBalance(ctx context.Context, id string, balance domain.Money) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	w, ok := f.wallets[id]
	if !ok {
		return domain.ErrWalletNotFound
	}
	w.Balance = balance
	w.UpdatedAt = time.Now().UTC()
	return nil
}

// fakeTransfers is a thread-safe in-memory TransferRepository.
type fakeTransfers struct {
	mu        sync.Mutex
	transfers map[string]*domain.Transfer
	createErr error // when set, Create fails (fault injection)
}

func newFakeTransfers() *fakeTransfers {
	return &fakeTransfers{transfers: make(map[string]*domain.Transfer)}
}

func (f *fakeTransfers) Create(ctx context.Context, transfer *domain.Transfer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	cp := *transfer
	f.transfers[transfer.ID] = &cp
	return nil
}

func (f *fakeTransfers) Get(ctx context.Context, id string) (*domain.Transfer, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	t, ok := f.transfers[id]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	cp := *t
	return &cp, nil
}

func (f *fakeTransfers) Update(ctx context.Context, transfer *domain.Transfer) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.transfers[transfer.ID]; !ok {
		return domain.ErrTransferNotFound
	}
	cp := *transfer
	f.transfers[transfer.ID] = &cp
	return nil
}

// fakeLedger is a thread-safe in-memory LedgerRepository.
type fakeLedger struct {
	mu        sync.Mutex
	entries   []domain.LedgerEntry
	createErr error // when set, CreateEntries fails (fault injection)
}

func newFakeLedger() *fakeLedger {
	return &fakeLedger{}
}

func (f *fakeLedger) CreateEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.createErr != nil {
		return f.createErr
	}
	f.entries = append(f.entries, entries...)
	return nil
}

func (f *fakeLedger) ListByWallet(ctx context.Context, walletID string) ([]domain.LedgerEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []domain.LedgerEntry
	for i := len(f.entries) - 1; i >= 0; i-- {
		if f.entries[i].WalletID == walletID {
			out = append(out, f.entries[i])
		}
	}
	return out, nil
}

func (f *fakeLedger) all() []domain.LedgerEntry {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]domain.LedgerEntry, len(f.entries))
	copy(out, f.entries)
	return out
}

// fakeIdempotency is a thread-safe in-memory IdempotencyRepository that
// simulates the database UNIQUE constraint on Key.
type fakeIdempotency struct {
	mu      sync.Mutex
	records map[string]*domain.IdempotencyRecord
}

func newFakeIdempotency() *fakeIdempotency {
	return &fakeIdempotency{records: make(map[string]*domain.IdempotencyRecord)}
}

func (f *fakeIdempotency) Insert(ctx context.Context, record *domain.IdempotencyRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.records[record.Key]; ok {
		return domain.ErrDuplicateIdempotencyKey
	}
	now := time.Now().UTC()
	cp := *record
	cp.CreatedAt = now
	cp.UpdatedAt = now
	f.records[record.Key] = &cp
	return nil
}

func (f *fakeIdempotency) Get(ctx context.Context, key string) (*domain.IdempotencyRecord, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.records[key]
	if !ok {
		return nil, domain.ErrTransferNotFound
	}
	cp := *r
	return &cp, nil
}

func (f *fakeIdempotency) Complete(ctx context.Context, key, transferID string, response []byte) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	r, ok := f.records[key]
	if !ok {
		return domain.ErrTransferNotFound
	}
	r.TransferID = transferID
	r.ResponseBody = response
	r.Status = domain.IdempotencyCompleted
	r.UpdatedAt = time.Now().UTC()
	return nil
}

// testHarness bundles the fakes and the service under test.
type testHarness struct {
	wallets     *fakeWallets
	transfers   *fakeTransfers
	ledger      *fakeLedger
	idempotency *fakeIdempotency
	svc         *Service
}

// newHarness wires the fakes into a Service with a deterministic, sequential id
// generator so produced ids are predictable across a test.
func newHarness() *testHarness {
	w := newFakeWallets()
	t := newFakeTransfers()
	l := newFakeLedger()
	idem := newFakeIdempotency()

	var counter int
	var mu sync.Mutex
	gen := func() string {
		mu.Lock()
		defer mu.Unlock()
		counter++
		return fmt.Sprintf("id-%04d", counter)
	}

	svc := New(Repositories{
		Wallets:     w,
		Transfers:   t,
		Ledger:      l,
		Idempotency: idem,
		TxManager:   newFakeTxManager(),
	}, WithIDGenerator(gen))

	return &testHarness{wallets: w, transfers: t, ledger: l, idempotency: idem, svc: svc}
}
