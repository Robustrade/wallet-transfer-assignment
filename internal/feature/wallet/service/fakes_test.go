package service_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/repository"

	"gorm.io/gorm"
)

// fakeStore is an in-memory stand-in for Postgres that enforces the same
// constraints the schema does (FKs, balance >= 0, guarded status update,
// UNIQUE keys) and supports rollback. Transactions are serialised by a mutex,
// so these tests exercise the service's *logic*; real locking behaviour is
// covered by the integration tests.
type fakeStore struct {
	mu sync.Mutex

	wallets   map[int64]int64
	transfers map[int64]*domain.Transfer
	ledger    []domain.LedgerEntry
	idem      map[string]*domain.IdempotencyRecord
	nextID    int64

	// fault injection
	txAttempts      int
	lockErrs        []error // consumed one per GetByIDForUpdate call
	ledgerErr       error
	commitErr       error // returned from WithinTransaction after fn succeeded
	commitApplied   bool  // whether the "uncertain" commit actually persisted
	lockCalls       int
	faults          map[string]error // named one-shot-free faults, see fault()
	ledgerCallCount int
}

func newStore(balances map[int64]int64) *fakeStore {
	s := &fakeStore{
		wallets:   map[int64]int64{},
		transfers: map[int64]*domain.Transfer{},
		idem:      map[string]*domain.IdempotencyRecord{},
	}
	for id, b := range balances {
		s.wallets[id] = b
	}
	return s
}

func (s *fakeStore) id() int64 { s.nextID++; return s.nextID }

// fault returns the error injected for the named step, or nil. Recognised
// names: debit, credit, createTransfer, updateStatus, saveResponse.
func (s *fakeStore) fault(name string) error { return s.faults[name] }

type snapshot struct {
	wallets   map[int64]int64
	transfers map[int64]domain.Transfer
	ledger    []domain.LedgerEntry
	idem      map[string]domain.IdempotencyRecord
	nextID    int64
}

func (s *fakeStore) snap() snapshot {
	sn := snapshot{
		wallets:   map[int64]int64{},
		transfers: map[int64]domain.Transfer{},
		idem:      map[string]domain.IdempotencyRecord{},
		ledger:    append([]domain.LedgerEntry(nil), s.ledger...),
		nextID:    s.nextID,
	}
	for k, v := range s.wallets {
		sn.wallets[k] = v
	}
	for k, v := range s.transfers {
		sn.transfers[k] = *v
	}
	for k, v := range s.idem {
		sn.idem[k] = *v
	}
	return sn
}

func (s *fakeStore) restore(sn snapshot) {
	s.wallets = sn.wallets
	s.ledger = sn.ledger
	s.nextID = sn.nextID
	s.transfers = map[int64]*domain.Transfer{}
	for k, v := range sn.transfers {
		v := v
		s.transfers[k] = &v
	}
	s.idem = map[string]*domain.IdempotencyRecord{}
	for k, v := range sn.idem {
		v := v
		s.idem[k] = &v
	}
}

// ---- transaction manager -------------------------------------------------

type fakeTxManager struct{ s *fakeStore }

func (m fakeTxManager) WithinTransaction(_ context.Context, fn func(tx *gorm.DB) error) error {
	m.s.mu.Lock()
	defer m.s.mu.Unlock()

	m.s.txAttempts++
	before := m.s.snap()

	if err := fn(nil); err != nil {
		m.s.restore(before) // rollback
		return err
	}
	if m.s.commitErr != nil {
		err := m.s.commitErr
		m.s.commitErr = nil
		if !m.s.commitApplied {
			m.s.restore(before)
		}
		return fmt.Errorf("%w: %w", repository.ErrCommitUncertain, err)
	}
	return nil
}

// ---- wallet repo ---------------------------------------------------------

type fakeWalletRepo struct{ s *fakeStore }

func (r fakeWalletRepo) GetByID(_ context.Context, _ *gorm.DB, id int64) (*domain.Wallet, error) {
	b, ok := r.s.wallets[id]
	if !ok {
		return nil, domain.ErrWalletNotFound
	}
	return &domain.Wallet{ID: id, Balance: b}, nil
}

func (r fakeWalletRepo) GetByIDForUpdate(ctx context.Context, tx *gorm.DB, id int64) (*domain.Wallet, error) {
	r.s.lockCalls++
	if len(r.s.lockErrs) > 0 {
		err := r.s.lockErrs[0]
		r.s.lockErrs = r.s.lockErrs[1:]
		if err != nil {
			return nil, fmt.Errorf("lock wallet: %w", err)
		}
	}
	return r.GetByID(ctx, tx, id)
}

func (r fakeWalletRepo) UpdateBalance(_ context.Context, _ *gorm.DB, id, delta int64) error {
	name := "credit"
	if delta < 0 {
		name = "debit"
	}
	if err := r.s.fault(name); err != nil {
		return err
	}
	b, ok := r.s.wallets[id]
	if !ok {
		return errors.New("wallet not found or insufficient balance")
	}
	if b+delta < 0 { // CHECK (balance >= 0)
		return errors.New("violates check constraint chk_wallet_balance")
	}
	r.s.wallets[id] = b + delta
	return nil
}

// ---- transfer repo -------------------------------------------------------

type fakeTransferRepo struct{ s *fakeStore }

func (r fakeTransferRepo) Create(_ context.Context, _ *gorm.DB, t *domain.Transfer) (bool, error) {
	if err := r.s.fault("createTransfer"); err != nil {
		return false, err
	}
	for _, ex := range r.s.transfers {
		if ex.IdempotencyKey == t.IdempotencyKey {
			return false, nil
		}
	}
	// FK: both wallets must exist
	if _, ok := r.s.wallets[t.FromWalletID]; !ok {
		return false, errors.New("violates foreign key constraint fk_transfers_from_wallet")
	}
	if _, ok := r.s.wallets[t.ToWalletID]; !ok {
		return false, errors.New("violates foreign key constraint fk_transfers_to_wallet")
	}
	cp := *t
	cp.ID = r.s.id()
	cp.Status = domain.TransferPending
	r.s.transfers[cp.ID] = &cp
	t.ID = cp.ID
	t.Status = domain.TransferPending
	return true, nil
}

func (r fakeTransferRepo) GetByIdempotencyKey(_ context.Context, _ *gorm.DB, key string) (*domain.Transfer, error) {
	for _, t := range r.s.transfers {
		if t.IdempotencyKey == key {
			cp := *t
			return &cp, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func (r fakeTransferRepo) UpdateStatus(_ context.Context, _ *gorm.DB, t *domain.Transfer) error {
	if err := r.s.fault("updateStatus"); err != nil {
		return err
	}
	if t.Status != domain.TransferProcessed && t.Status != domain.TransferFailed {
		return domain.ErrInvalidTransition
	}
	cur, ok := r.s.transfers[t.ID]
	if !ok || cur.Status != domain.TransferPending { // WHERE id = ? AND status = 'PENDING'
		return domain.ErrInvalidTransition
	}
	cur.Status = t.Status
	cur.FailureReason = t.FailureReason
	return nil
}

// ---- ledger repo ---------------------------------------------------------

type fakeLedgerRepo struct{ s *fakeStore }

func (r fakeLedgerRepo) CreateEntries(_ context.Context, _ *gorm.DB, entries []domain.LedgerEntry) error {
	r.s.ledgerCallCount++
	if r.s.ledgerErr != nil {
		return r.s.ledgerErr
	}
	if len(entries) != 2 {
		return domain.ErrInvalidLedgerEntry
	}
	for _, e := range entries {
		if _, ok := r.s.transfers[e.TransferID]; !ok { // FK fk_ledger_transfer
			return errors.New("violates foreign key constraint fk_ledger_transfer")
		}
		if err := e.Validate(); err != nil {
			return err
		}
		for _, ex := range r.s.ledger { // UNIQUE (transfer_id, entry_type)
			if ex.TransferID == e.TransferID && ex.EntryType == e.EntryType {
				return errors.New("violates unique constraint uq_ledger_transfer_entry_type")
			}
		}
	}
	for _, e := range entries {
		e.ID = r.s.id()
		r.s.ledger = append(r.s.ledger, e)
	}
	return nil
}

func (r fakeLedgerRepo) GetByTransferID(_ context.Context, _ *gorm.DB, id int64) ([]domain.LedgerEntry, error) {
	var out []domain.LedgerEntry
	for _, e := range r.s.ledger {
		if e.TransferID == id {
			out = append(out, e)
		}
	}
	return out, nil
}

// ---- idempotency repo ----------------------------------------------------

type fakeIdempotencyRepo struct{ s *fakeStore }

func (r fakeIdempotencyRepo) Create(_ context.Context, _ *gorm.DB, rec *domain.IdempotencyRecord) (bool, error) {
	if _, exists := r.s.idem[rec.IdempotencyKey]; exists {
		return false, nil
	}
	cp := *rec
	cp.ID = r.s.id()
	r.s.idem[cp.IdempotencyKey] = &cp
	rec.ID = cp.ID
	return true, nil
}

func (r fakeIdempotencyRepo) GetByKey(_ context.Context, _ *gorm.DB, key string) (*domain.IdempotencyRecord, error) {
	rec, ok := r.s.idem[key]
	if !ok {
		return nil, domain.ErrIdempotencyNotFound
	}
	cp := *rec
	return &cp, nil
}

func (r fakeIdempotencyRepo) SaveResponse(_ context.Context, _ *gorm.DB, rec *domain.IdempotencyRecord) error {
	if err := r.s.fault("saveResponse"); err != nil {
		return err
	}
	if rec.ResponseStatus == nil || len(rec.ResponseBody) == 0 {
		return domain.ErrInvalidIdempotency
	}
	cur, ok := r.s.idem[rec.IdempotencyKey]
	if !ok || cur.ResponseStatus != nil { // WHERE response_status IS NULL
		return domain.ErrIdempotencyConflict
	}
	if rec.TransferID != nil {
		if _, ok := r.s.transfers[*rec.TransferID]; !ok { // FK fk_idempotency_transfer
			return errors.New("violates foreign key constraint fk_idempotency_transfer")
		}
	}
	status := *rec.ResponseStatus
	cur.TransferID = rec.TransferID
	cur.ResponseStatus = &status
	cur.ResponseBody = json.RawMessage(append([]byte(nil), rec.ResponseBody...))
	return nil
}
