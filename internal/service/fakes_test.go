package service_test

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
	"github.com/devthedeveloper/wallet-transfer-assignment/internal/service"
)

// fakeStore is an in-memory backing store shared by every fake repository
// and the fake UnitOfWork, so writes made inside one Do call are visible to
// reads made inside the same call, the way a real transaction's statements
// see each other's writes.
type fakeStore struct {
	mu             sync.Mutex
	wallets        map[string]domain.Wallet
	transfersByID  map[uuid.UUID]domain.Transfer
	transfersByKey map[string]uuid.UUID
	ledger         []domain.LedgerEntry
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		wallets:        map[string]domain.Wallet{},
		transfersByID:  map[uuid.UUID]domain.Transfer{},
		transfersByKey: map[string]uuid.UUID{},
	}
}

func (s *fakeStore) snapshot() *fakeStore {
	cp := newFakeStore()
	maps.Copy(cp.wallets, s.wallets)
	maps.Copy(cp.transfersByID, s.transfersByID)
	maps.Copy(cp.transfersByKey, s.transfersByKey)
	cp.ledger = append(cp.ledger, s.ledger...)
	return cp
}

func (s *fakeStore) restore(from *fakeStore) {
	s.wallets = from.wallets
	s.transfersByID = from.transfersByID
	s.transfersByKey = from.transfersByKey
	s.ledger = from.ledger
}

// fakeUnitOfWork implements service.UnitOfWork. Do snapshots the store
// before invoking fn and restores the snapshot if fn returns an error,
// mirroring a real transaction's commit/rollback semantics closely enough
// to unit test the service's orchestration logic without a database.
type fakeUnitOfWork struct {
	store *fakeStore
}

func newFakeUnitOfWork() *fakeUnitOfWork {
	return &fakeUnitOfWork{store: newFakeStore()}
}

func (u *fakeUnitOfWork) Wallets() service.WalletRepository { return &fakeWalletRepo{store: u.store} }
func (u *fakeUnitOfWork) Transfers() service.TransferRepository {
	return &fakeTransferRepo{store: u.store}
}
func (u *fakeUnitOfWork) Ledger() service.LedgerRepository { return &fakeLedgerRepo{store: u.store} }

func (u *fakeUnitOfWork) Do(ctx context.Context, fn func(ctx context.Context, r service.Repos) error) error {
	u.store.mu.Lock()
	backup := u.store.snapshot()
	u.store.mu.Unlock()

	err := fn(ctx, &fakeRepos{store: u.store})

	if err != nil {
		u.store.mu.Lock()
		u.store.restore(backup)
		u.store.mu.Unlock()
	}
	return err
}

// retryingUnitOfWork wraps a fakeUnitOfWork and fails the first
// failAttempts calls to Do with a transient error before delegating for
// real, to unit test that TransferService.Create behaves correctly when the
// transaction runner retries it — see docs/design.md §7's retry policy.
type retryingUnitOfWork struct {
	*fakeUnitOfWork
	failAttempts int
	attempts     int
}

func (u *retryingUnitOfWork) Do(ctx context.Context, fn func(ctx context.Context, r service.Repos) error) error {
	// The retry loop lives inside a single Do call, mirroring
	// postgres.DB.Do: a caller of UnitOfWork.Do sees one call in, one
	// result out, regardless of how many attempts it took underneath.
	for {
		u.attempts++
		if u.attempts <= u.failAttempts {
			continue
		}
		return u.fakeUnitOfWork.Do(ctx, fn)
	}
}

type fakeRepos struct {
	store *fakeStore
}

func (r *fakeRepos) Wallets() service.WalletRepository     { return &fakeWalletRepo{store: r.store} }
func (r *fakeRepos) Transfers() service.TransferRepository { return &fakeTransferRepo{store: r.store} }
func (r *fakeRepos) Ledger() service.LedgerRepository      { return &fakeLedgerRepo{store: r.store} }

type fakeWalletRepo struct{ store *fakeStore }

func (r *fakeWalletRepo) Get(_ context.Context, id string) (domain.Wallet, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	w, ok := r.store.wallets[id]
	if !ok {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return w, nil
}

// GetForUpdate has no real locking in the fake — concurrency safety is
// proven by the real-database integration suite, not by unit tests.
func (r *fakeWalletRepo) GetForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	return r.Get(ctx, id)
}

func (r *fakeWalletRepo) Create(_ context.Context, w domain.Wallet) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.wallets[w.ID]; ok {
		return domain.ErrWalletAlreadyExists
	}
	r.store.wallets[w.ID] = w
	return nil
}

func (r *fakeWalletRepo) ApplyDelta(_ context.Context, id string, delta int64) (int64, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	w, ok := r.store.wallets[id]
	if !ok {
		return 0, domain.ErrWalletNotFound
	}
	if w.Kind != domain.WalletKindSystem && w.BalanceMinor+delta < 0 {
		return 0, domain.ErrInsufficientFunds
	}
	w.BalanceMinor += delta
	r.store.wallets[id] = w
	return w.BalanceMinor, nil
}

type fakeTransferRepo struct{ store *fakeStore }

func (r *fakeTransferRepo) InsertIfAbsent(_ context.Context, t domain.Transfer) (bool, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	if _, ok := r.store.transfersByKey[t.IdempotencyKey]; ok {
		return false, nil
	}
	r.store.transfersByID[t.ID] = t
	r.store.transfersByKey[t.IdempotencyKey] = t.ID
	return true, nil
}

func (r *fakeTransferRepo) FindByID(_ context.Context, id uuid.UUID) (domain.Transfer, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	t, ok := r.store.transfersByID[id]
	if !ok {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return t, nil
}

func (r *fakeTransferRepo) FindByIdempotencyKey(_ context.Context, key string) (domain.Transfer, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	id, ok := r.store.transfersByKey[key]
	if !ok {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return r.store.transfersByID[id], nil
}

func (r *fakeTransferRepo) MarkProcessed(_ context.Context, id uuid.UUID, settledAt time.Time) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	t, ok := r.store.transfersByID[id]
	if !ok || t.Status != domain.TransferStatusPending {
		return fmt.Errorf("%w: transfer %s not pending", domain.ErrIllegalStateChange, id)
	}
	t.Status = domain.TransferStatusProcessed
	t.SettledAt = &settledAt
	r.store.transfersByID[id] = t
	return nil
}

func (r *fakeTransferRepo) MarkFailed(_ context.Context, id uuid.UUID, code domain.FailureCode, message string) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	t, ok := r.store.transfersByID[id]
	if !ok || t.Status != domain.TransferStatusPending {
		return fmt.Errorf("%w: transfer %s not pending", domain.ErrIllegalStateChange, id)
	}
	t.Status = domain.TransferStatusFailed
	t.FailureCode = code
	t.FailureMessage = message
	r.store.transfersByID[id] = t
	return nil
}

type fakeLedgerRepo struct{ store *fakeStore }

func (r *fakeLedgerRepo) InsertPair(_ context.Context, debit, credit domain.LedgerEntry) error {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	debit.ID = int64(len(r.store.ledger) + 1)
	r.store.ledger = append(r.store.ledger, debit)
	credit.ID = int64(len(r.store.ledger) + 1)
	r.store.ledger = append(r.store.ledger, credit)
	return nil
}

func (r *fakeLedgerRepo) ListByWallet(_ context.Context, walletID string, limit int, beforeID *int64) ([]domain.LedgerEntry, error) {
	r.store.mu.Lock()
	defer r.store.mu.Unlock()
	var out []domain.LedgerEntry
	for _, e := range slices.Backward(r.store.ledger) {
		if e.WalletID != walletID {
			continue
		}
		if beforeID != nil && e.ID >= *beforeID {
			continue
		}
		out = append(out, e)
		if len(out) == limit {
			break
		}
	}
	return out, nil
}
