package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/repository"
)

// --- test doubles -----------------------------------------------------

type fakeTx struct {
	execCalls  int
	committed  bool
	rolledBack bool
}

func (t *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	t.execCalls++
	return pgconn.CommandTag{}, nil
}
func (t *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return nil }
func (t *fakeTx) Commit(_ context.Context) error {
	t.committed = true
	return nil
}
func (t *fakeTx) Rollback(_ context.Context) error {
	t.rolledBack = true
	return nil
}

type fakeTransactor struct {
	tx       *fakeTx
	beginErr error
}

func (f *fakeTransactor) Begin(_ context.Context) (repository.Tx, error) {
	if f.beginErr != nil {
		return nil, f.beginErr
	}
	f.tx = &fakeTx{}
	return f.tx, nil
}

type mockWalletRepo struct {
	wallets           []domain.Wallet
	getForUpdateErr   error
	updateBalancesErr error
	updateCalls       []struct {
		fromWalletID string
		fromBalance  decimal.Decimal
		toWalletID   string
		toBalance    decimal.Decimal
	}
}

func (m *mockWalletRepo) GetForUpdate(_ context.Context, _ repository.Tx, _ ...string) ([]domain.Wallet, error) {
	if m.getForUpdateErr != nil {
		return nil, m.getForUpdateErr
	}
	return m.wallets, nil
}

func (m *mockWalletRepo) UpdateBalances(_ context.Context, _ repository.Tx, fromWalletID string, fromBalance decimal.Decimal, toWalletID string, toBalance decimal.Decimal) error {
	m.updateCalls = append(m.updateCalls, struct {
		fromWalletID string
		fromBalance  decimal.Decimal
		toWalletID   string
		toBalance    decimal.Decimal
	}{fromWalletID, fromBalance, toWalletID, toBalance})
	return m.updateBalancesErr
}

type mockTransferRepo struct {
	insertTransferResult *domain.Transfer
	insertTransferErr    error
	insertTransferCalls  int

	// findByIDResults is consumed in order across successive FindByID
	// calls (the pre-check, then — only if reached — the post-race
	// recheck); the last entry repeats once exhausted.
	findByIDResults []*domain.Transfer
	findByIDErr     error
	findByIDCalls   int

	insertLedgerErr   error
	insertLedgerCalls int
}

func (m *mockTransferRepo) InsertTransfer(_ context.Context, _ repository.Tx, _ int64, _, _, _ string, _ decimal.Decimal, _ domain.Status, _ *domain.ErrorCode) (*domain.Transfer, error) {
	m.insertTransferCalls++
	if m.insertTransferErr != nil {
		return nil, m.insertTransferErr
	}
	return m.insertTransferResult, nil
}

func (m *mockTransferRepo) FindByID(_ context.Context, _ int64) (*domain.Transfer, error) {
	if m.findByIDErr != nil {
		return nil, m.findByIDErr
	}
	if len(m.findByIDResults) == 0 {
		return nil, nil
	}
	idx := m.findByIDCalls
	if idx >= len(m.findByIDResults) {
		idx = len(m.findByIDResults) - 1
	}
	m.findByIDCalls++
	return m.findByIDResults[idx], nil
}

func (m *mockTransferRepo) InsertLedgerEntries(_ context.Context, _ repository.Tx, _ int64, _, _ string, _ decimal.Decimal) error {
	m.insertLedgerCalls++
	return m.insertLedgerErr
}

// mockCache guards setCalls with a mutex because writeCache (the only
// caller of Set) now runs it in its own goroutine, off the request's
// hot path -- Set can race with a test's own goroutine reading back
// what was recorded.
type mockCache struct {
	mu        sync.Mutex
	getResult *domain.Transfer
	getErr    error
	setErr    error
	setCalls  []*domain.Transfer
}

func (m *mockCache) Get(_ context.Context, _ int64) (*domain.Transfer, error) {
	return m.getResult, m.getErr
}

func (m *mockCache) Set(_ context.Context, _ int64, transfer *domain.Transfer) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.setCalls = append(m.setCalls, transfer)
	return m.setErr
}

func (m *mockCache) SetCalls() []*domain.Transfer {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]*domain.Transfer(nil), m.setCalls...)
}

// waitForCacheSet polls until at least one cache Set call has been
// recorded, or fails the test after a short timeout -- writeCache is
// asynchronous, so its effect isn't visible the instant the call that
// triggered it returns.
func waitForCacheSet(t *testing.T, c *mockCache) []*domain.Transfer {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		calls := c.SetCalls()
		if len(calls) >= 1 {
			return calls
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for a cache Set call, got %d", len(calls))
		}
		time.Sleep(time.Millisecond)
	}
}

type mockIDGen struct {
	id  int64
	err error
}

func (m *mockIDGen) NextID() (int64, error) {
	if m.err != nil {
		return 0, m.err
	}
	return m.id, nil
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func newTestService(db *fakeTransactor, wallets *mockWalletRepo, transfers *mockTransferRepo, c *mockCache) *TransferService {
	return NewTransferService(db, wallets, transfers, c, &mockIDGen{id: 1}, 5*time.Second, testLogger())
}

// --- request builders ---------------------------------------------------

func validRequest() TransferRequest {
	return TransferRequest{
		ID:               42,
		FromWalletID:     "wallet-a",
		ToWalletID:       "wallet-b",
		Amount:           decimal.RequireFromString("10.00"),
		AmountMinorUnits: 1000,
	}
}

func requireDomainErrCode(t *testing.T, err error, code domain.ErrorCode) {
	t.Helper()
	var domainErr *domain.Error
	if !errors.As(err, &domainErr) {
		t.Fatalf("expected *domain.Error, got %T (%v)", err, err)
	}
	if domainErr.Code != code {
		t.Fatalf("expected code %s, got %s", code, domainErr.Code)
	}
}

// --- Step 0: deterministic checks ---------------------------------------

func TestTransfer_SameWallet(t *testing.T) {
	db := &fakeTransactor{}
	c := &mockCache{}
	svc := newTestService(db, &mockWalletRepo{}, &mockTransferRepo{}, c)

	req := validRequest()
	req.ToWalletID = req.FromWalletID

	_, err := svc.Transfer(context.Background(), req)
	requireDomainErrCode(t, err, domain.ErrCodeSameWallet)

	if len(c.SetCalls()) != 0 || db.tx != nil {
		t.Fatalf("same-wallet check must short-circuit before any cache/DB call")
	}
}

func TestTransfer_InvalidAmount(t *testing.T) {
	db := &fakeTransactor{}
	c := &mockCache{}
	svc := newTestService(db, &mockWalletRepo{}, &mockTransferRepo{}, c)

	req := validRequest()
	req.AmountMinorUnits = 0

	_, err := svc.Transfer(context.Background(), req)
	requireDomainErrCode(t, err, domain.ErrCodeInvalidAmount)

	if db.tx != nil {
		t.Fatalf("invalid-amount check must short-circuit before any DB call")
	}
}

// --- cache path ----------------------------------------------------------

func TestTransfer_CacheHit_Replay(t *testing.T) {
	req := validRequest()
	hash := computeHash(req.FromWalletID, req.ToWalletID, req.AmountMinorUnits)
	existing := &domain.Transfer{ID: req.ID, RequestHash: hash, Status: domain.StatusProcessed}

	db := &fakeTransactor{}
	svc := newTestService(db, &mockWalletRepo{}, &mockTransferRepo{}, &mockCache{getResult: existing})

	got, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != existing {
		t.Fatalf("expected cached transfer to be returned as-is")
	}
	if db.tx != nil {
		t.Fatalf("cache hit must never open a DB transaction")
	}
}

func TestTransfer_CacheHit_HashMismatch(t *testing.T) {
	req := validRequest()
	existing := &domain.Transfer{ID: req.ID, RequestHash: "different-hash", Status: domain.StatusProcessed}

	svc := newTestService(&fakeTransactor{}, &mockWalletRepo{}, &mockTransferRepo{}, &mockCache{getResult: existing})

	_, err := svc.Transfer(context.Background(), req)
	requireDomainErrCode(t, err, domain.ErrCodeIdempotencyKeyReused)
}

func TestTransfer_CacheGetError_FallsBackToPostgres(t *testing.T) {
	// Cache is a latency optimization only (docs/DESIGN.md §6) -- a Redis
	// failure must never fail a request that Postgres can still answer.
	req := validRequest()
	hash := computeHash(req.FromWalletID, req.ToWalletID, req.AmountMinorUnits)
	existing := &domain.Transfer{ID: req.ID, RequestHash: hash, Status: domain.StatusProcessed}

	db := &fakeTransactor{}
	transfers := &mockTransferRepo{findByIDResults: []*domain.Transfer{existing}}
	c := &mockCache{getErr: errors.New("redis unavailable")}
	svc := newTestService(db, &mockWalletRepo{}, transfers, c)

	got, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != existing {
		t.Fatalf("expected fallback to Postgres to replay the existing transfer")
	}
}

// --- pre-transaction existing-transfer check (cache miss, id already
// committed) -- must resolve without ever touching wallets ---------------

func TestTransfer_ExistingTransfer_Replay(t *testing.T) {
	req := validRequest()
	hash := computeHash(req.FromWalletID, req.ToWalletID, req.AmountMinorUnits)
	existing := &domain.Transfer{ID: req.ID, RequestHash: hash, Status: domain.StatusProcessed}

	db := &fakeTransactor{}
	wallets := &mockWalletRepo{}
	transfers := &mockTransferRepo{findByIDResults: []*domain.Transfer{existing}}
	c := &mockCache{}
	svc := newTestService(db, wallets, transfers, c)

	got, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != existing {
		t.Fatalf("expected existing transfer to be replayed")
	}
	waitForCacheSet(t, c)
	if db.tx != nil {
		t.Fatalf("an already-committed id must never open a DB transaction or touch wallet locks")
	}
	if len(wallets.updateCalls) != 0 {
		t.Fatalf("an already-committed id must never touch wallet balances")
	}
}

func TestTransfer_ExistingTransfer_HashMismatch(t *testing.T) {
	req := validRequest()
	existing := &domain.Transfer{ID: req.ID, RequestHash: "different-hash", Status: domain.StatusProcessed}

	db := &fakeTransactor{}
	transfers := &mockTransferRepo{findByIDResults: []*domain.Transfer{existing}}
	svc := newTestService(db, &mockWalletRepo{}, transfers, &mockCache{})

	_, err := svc.Transfer(context.Background(), req)
	requireDomainErrCode(t, err, domain.ErrCodeIdempotencyKeyReused)

	if db.tx != nil {
		t.Fatalf("a hash-mismatched already-committed id must never open a DB transaction")
	}
}

// --- claim-race path (pre-check found nothing, but InsertTransfer still
// lost a genuine concurrent race) ------------------------------------------

func TestTransfer_ClaimRace_Replay(t *testing.T) {
	req := validRequest()
	hash := computeHash(req.FromWalletID, req.ToWalletID, req.AmountMinorUnits)
	winner := &domain.Transfer{ID: req.ID, RequestHash: hash, Status: domain.StatusProcessed}

	db := &fakeTransactor{}
	wallets := &mockWalletRepo{wallets: []domain.Wallet{
		{ID: req.FromWalletID, Balance: decimal.RequireFromString("100.00")},
		{ID: req.ToWalletID, Balance: decimal.RequireFromString("5.00")},
	}}
	// First FindByID (pre-check) sees nothing; second (post-race) sees the winner.
	transfers := &mockTransferRepo{
		insertTransferResult: nil,
		findByIDResults:      []*domain.Transfer{nil, winner},
	}
	c := &mockCache{}
	svc := newTestService(db, wallets, transfers, c)

	got, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != winner {
		t.Fatalf("expected the race winner's transfer to be replayed")
	}
	if transfers.insertTransferCalls != 1 {
		t.Fatalf("expected exactly one InsertTransfer attempt, got %d", transfers.insertTransferCalls)
	}
}

func TestTransfer_ClaimRace_HashMismatch(t *testing.T) {
	req := validRequest()
	winner := &domain.Transfer{ID: req.ID, RequestHash: "different-hash", Status: domain.StatusProcessed}

	db := &fakeTransactor{}
	wallets := &mockWalletRepo{wallets: []domain.Wallet{
		{ID: req.FromWalletID, Balance: decimal.RequireFromString("100.00")},
		{ID: req.ToWalletID, Balance: decimal.RequireFromString("5.00")},
	}}
	transfers := &mockTransferRepo{
		insertTransferResult: nil,
		findByIDResults:      []*domain.Transfer{nil, winner},
	}
	svc := newTestService(db, wallets, transfers, &mockCache{})

	_, err := svc.Transfer(context.Background(), req)
	requireDomainErrCode(t, err, domain.ErrCodeIdempotencyKeyReused)
}

// --- fresh-claim path ------------------------------------------------------

func TestTransfer_WalletNotFound(t *testing.T) {
	req := validRequest()

	db := &fakeTransactor{}
	wallets := &mockWalletRepo{wallets: nil} // neither wallet exists
	transfers := &mockTransferRepo{}
	svc := newTestService(db, wallets, transfers, &mockCache{})

	_, err := svc.Transfer(context.Background(), req)
	requireDomainErrCode(t, err, domain.ErrCodeWalletNotFound)

	if !db.tx.rolledBack || db.tx.committed {
		t.Fatalf("wallet-not-found must roll back, never commit")
	}
	if transfers.insertTransferCalls != 0 {
		t.Fatalf("wallet-not-found must never attempt to claim the transfer")
	}
}

func TestTransfer_InsufficientBalance(t *testing.T) {
	req := validRequest()
	from := domain.Wallet{ID: req.FromWalletID, Balance: decimal.RequireFromString("1.00")}
	to := domain.Wallet{ID: req.ToWalletID, Balance: decimal.RequireFromString("0.00")}
	reason := domain.ErrCodeInsufficientBalance
	failed := &domain.Transfer{ID: req.ID, Status: domain.StatusFailed, FailureReason: &reason}

	db := &fakeTransactor{}
	wallets := &mockWalletRepo{wallets: []domain.Wallet{from, to}}
	transfers := &mockTransferRepo{insertTransferResult: failed}
	c := &mockCache{}
	svc := newTestService(db, wallets, transfers, c)

	got, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("insufficient balance is a persisted outcome, not a returned error: %v", err)
	}
	if got.Status != domain.StatusFailed || got.FailureReason == nil || *got.FailureReason != domain.ErrCodeInsufficientBalance {
		t.Fatalf("expected a persisted FAILED/INSUFFICIENT_BALANCE transfer, got %+v", got)
	}
	if !db.tx.committed {
		t.Fatalf("insufficient-balance outcome must still be committed")
	}
	if len(wallets.updateCalls) != 0 {
		t.Fatalf("insufficient-balance must never debit or credit")
	}
	if transfers.insertLedgerCalls != 0 {
		t.Fatalf("insufficient-balance must never write ledger entries")
	}
	waitForCacheSet(t, c)
}

func TestTransfer_Success(t *testing.T) {
	req := validRequest()
	from := domain.Wallet{ID: req.FromWalletID, Balance: decimal.RequireFromString("100.00")}
	to := domain.Wallet{ID: req.ToWalletID, Balance: decimal.RequireFromString("5.00")}
	processed := &domain.Transfer{ID: req.ID, Status: domain.StatusProcessed}

	db := &fakeTransactor{}
	wallets := &mockWalletRepo{wallets: []domain.Wallet{from, to}}
	transfers := &mockTransferRepo{insertTransferResult: processed}
	c := &mockCache{}
	svc := newTestService(db, wallets, transfers, c)

	got, err := svc.Transfer(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got.Status != domain.StatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", got.Status)
	}
	if len(wallets.updateCalls) != 1 {
		t.Fatalf("expected a single combined UpdateBalances call, got %d", len(wallets.updateCalls))
	}
	call := wallets.updateCalls[0]
	if !call.fromBalance.Equal(decimal.RequireFromString("90.00")) {
		t.Fatalf("expected debited balance 90.00, got %s", call.fromBalance)
	}
	if !call.toBalance.Equal(decimal.RequireFromString("15.00")) {
		t.Fatalf("expected credited balance 15.00, got %s", call.toBalance)
	}
	if transfers.insertLedgerCalls != 1 {
		t.Fatalf("expected exactly one InsertLedgerEntries call, got %d", transfers.insertLedgerCalls)
	}
	if !db.tx.committed {
		t.Fatalf("success must commit")
	}
	waitForCacheSet(t, c)
}

// --- GetTransfer / InitTransfer ------------------------------------------

func TestGetTransfer_CacheHit_SkipsPostgres(t *testing.T) {
	want := &domain.Transfer{ID: 7, Status: domain.StatusProcessed}
	transfers := &mockTransferRepo{}
	c := &mockCache{getResult: want}
	svc := newTestService(&fakeTransactor{}, &mockWalletRepo{}, transfers, c)

	got, err := svc.GetTransfer(context.Background(), 7)
	if err != nil || got != want {
		t.Fatalf("expected (%v, nil), got (%v, %v)", want, got, err)
	}
	if transfers.findByIDCalls != 0 {
		t.Fatalf("a cache hit must never touch Postgres, got %d FindByID calls", transfers.findByIDCalls)
	}
}

func TestGetTransfer_CacheMiss_FallsThroughAndSelfHeals(t *testing.T) {
	want := &domain.Transfer{ID: 7, Status: domain.StatusProcessed}
	transfers := &mockTransferRepo{findByIDResults: []*domain.Transfer{want}}
	c := &mockCache{}
	svc := newTestService(&fakeTransactor{}, &mockWalletRepo{}, transfers, c)

	got, err := svc.GetTransfer(context.Background(), 7)
	if err != nil || got != want {
		t.Fatalf("expected (%v, nil), got (%v, %v)", want, got, err)
	}
	calls := waitForCacheSet(t, c)
	if calls[0] != want {
		t.Fatalf("expected the cache to be self-healed with the Postgres result, got %+v", calls[0])
	}
}

func TestGetTransfer_CacheGetError_FallsBackToPostgres(t *testing.T) {
	want := &domain.Transfer{ID: 7, Status: domain.StatusProcessed}
	transfers := &mockTransferRepo{findByIDResults: []*domain.Transfer{want}}
	c := &mockCache{getErr: errors.New("redis unavailable")}
	svc := newTestService(&fakeTransactor{}, &mockWalletRepo{}, transfers, c)

	got, err := svc.GetTransfer(context.Background(), 7)
	if err != nil || got != want {
		t.Fatalf("expected (%v, nil), got (%v, %v)", want, got, err)
	}
}

func TestGetTransfer_NotFound(t *testing.T) {
	transfers := &mockTransferRepo{}
	svc := newTestService(&fakeTransactor{}, &mockWalletRepo{}, transfers, &mockCache{})

	got, err := svc.GetTransfer(context.Background(), 7)
	if err != nil || got != nil {
		t.Fatalf("expected (nil, nil), got (%v, %v)", got, err)
	}
}

func TestInitTransfer(t *testing.T) {
	db := &fakeTransactor{}
	svc := NewTransferService(db, &mockWalletRepo{}, &mockTransferRepo{}, &mockCache{}, &mockIDGen{id: 999}, 5*time.Second, testLogger())

	got, err := svc.InitTransfer()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != 999 {
		t.Fatalf("expected minted ID 999, got %d", got)
	}
}

func TestInitTransfer_IDGenError(t *testing.T) {
	db := &fakeTransactor{}
	wantErr := errors.New("clock moved backward")
	svc := NewTransferService(db, &mockWalletRepo{}, &mockTransferRepo{}, &mockCache{}, &mockIDGen{err: wantErr}, 5*time.Second, testLogger())

	_, err := svc.InitTransfer()
	requireDomainErrCode(t, err, domain.ErrCodeTransferInitFailed)
}
