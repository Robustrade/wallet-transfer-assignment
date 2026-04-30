package svcimpl_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apperr "github.com/Robustrade/wallet-transfer-assignment/common/error"
	"github.com/Robustrade/wallet-transfer-assignment/common/util/money"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/model"
	"github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/data/request"
	repoiface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/repo/iface"
	svciface "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/svc/ifaces"
	svcimpl "github.com/Robustrade/wallet-transfer-assignment/services/transfer/internal/svc/impls"
)

// -----------------------------------------------------------------------------
// In-memory fakes for the repositories and tx manager.
// -----------------------------------------------------------------------------

type fakeStore struct {
	mu        sync.Mutex
	wallets   map[string]*model.Wallet
	transfers map[uuid.UUID]*model.Transfer
	byKey     map[string]uuid.UUID
	ledger    []model.LedgerEntry
}

func newFakeStore(initial []model.Wallet) *fakeStore {
	s := &fakeStore{
		wallets:   make(map[string]*model.Wallet),
		transfers: make(map[uuid.UUID]*model.Transfer),
		byKey:     make(map[string]uuid.UUID),
	}
	for i := range initial {
		w := initial[i]
		s.wallets[w.ID] = &w
	}
	return s
}

type fakeWalletRepo struct{ s *fakeStore }

func (r *fakeWalletRepo) LockByIDs(_ context.Context, ids []string) (model.Wallets, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := make(model.Wallets, 0, len(ids))
	for _, id := range ids {
		w, ok := r.s.wallets[id]
		if !ok {
			return nil, apperr.ErrWalletNotFound
		}
		out = append(out, *w)
	}
	return out, nil
}

func (r *fakeWalletRepo) ApplyBalanceDeltas(_ context.Context, deltas []repoiface.BalanceDelta) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	// Two-phase apply matches the production behavior: validate every wallet
	// and the resulting balance, then commit. Returning early on the first
	// failure keeps prior wallet state untouched.
	type pending struct {
		w      *model.Wallet
		newBal money.Money
	}
	updates := make([]pending, 0, len(deltas))
	for _, d := range deltas {
		w, ok := r.s.wallets[d.WalletID]
		if !ok {
			return apperr.ErrWalletNotFound
		}
		newBal := w.Balance.Add(d.Delta)
		if newBal.IsNegative() {
			return apperr.ErrInsufficientFunds
		}
		updates = append(updates, pending{w: w, newBal: newBal})
	}
	for _, u := range updates {
		u.w.Balance = u.newBal
	}
	return nil
}

type fakeTransferRepo struct{ s *fakeStore }

func (r *fakeTransferRepo) Claim(_ context.Context, t model.Transfer) (bool, *model.Transfer, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if existingID, ok := r.s.byKey[t.IdempotencyKey]; ok {
		existing := *r.s.transfers[existingID]
		return false, &existing, nil
	}
	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	stored := t
	r.s.transfers[t.ID] = &stored
	r.s.byKey[t.IdempotencyKey] = t.ID
	return true, &stored, nil
}

func (r *fakeTransferRepo) UpdateOutcome(
	_ context.Context,
	id uuid.UUID,
	status model.TransferStatus,
	currency string,
	reason *string,
) (time.Time, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	t, ok := r.s.transfers[id]
	if !ok {
		return time.Time{}, errors.New("not found")
	}
	t.Status = status
	t.Currency = currency
	t.FailureReason = reason
	t.UpdatedAt = time.Now()
	return t.UpdatedAt, nil
}

type fakeLedgerRepo struct{ s *fakeStore }

func (r *fakeLedgerRepo) Append(_ context.Context, entries []model.LedgerEntry) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	r.s.ledger = append(r.s.ledger, entries...)
	return nil
}

// fakeTx runs the closure inline. Tests use a fresh store per case so we
// don't need transactional rollback semantics from the fake.
type fakeTx struct{}

func (fakeTx) Run(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// build returns a fully-wired service plus its underlying fake store.
func build(initial []model.Wallet) (svciface.TransferServiceIface, *fakeStore) {
	store := newFakeStore(initial)
	svc := svcimpl.NewTransferService(svcimpl.Deps{
		TxManager:    fakeTx{},
		WalletRepo:   &fakeWalletRepo{s: store},
		TransferRepo: &fakeTransferRepo{s: store},
		LedgerRepo:   &fakeLedgerRepo{s: store},
		Logger:       slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	return svc, store
}

// -----------------------------------------------------------------------------
// Fixtures
// -----------------------------------------------------------------------------

func initialWallets() []model.Wallet {
	return []model.Wallet{
		{ID: "wallet_a", Balance: money.FromMinor(10000), Currency: "INR"},
		{ID: "wallet_b", Balance: money.FromMinor(0), Currency: "INR"},
	}
}

func happyReq() request.CreateTransferReq {
	return request.CreateTransferReq{
		IdempotencyKey: "k1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         100,
	}
}

// -----------------------------------------------------------------------------
// Tests
// -----------------------------------------------------------------------------

// S1 — Happy path.
func TestS1_HappyPath(t *testing.T) {
	svc, store := build(initialWallets())

	resp, err := svc.CreateTransfer(context.Background(), happyReq())
	require.NoError(t, err)
	assert.Equal(t, "PROCESSED", resp.Status)
	assert.False(t, resp.Replayed)
	assert.Equal(t, int64(100), resp.Amount)
	assert.Equal(t, "INR", resp.Currency)

	// Timestamps must be populated from the DB-returned values, not zero.
	assert.False(t, resp.CreatedAt.IsZero(), "createdAt must be populated")
	assert.False(t, resp.UpdatedAt.IsZero(), "updatedAt must be populated")
	assert.False(t, resp.UpdatedAt.Before(resp.CreatedAt), "updatedAt must be >= createdAt")

	assert.Equal(t, int64(9900), store.wallets["wallet_a"].Balance.Minor())
	assert.Equal(t, int64(100), store.wallets["wallet_b"].Balance.Minor())
	assert.Len(t, store.ledger, 2)
	assert.Equal(t, model.EntryDebit, store.ledger[0].EntryType)
	assert.Equal(t, model.EntryCredit, store.ledger[1].EntryType)
}

// S2 — Insufficient funds → FAILED.
func TestS2_InsufficientFunds(t *testing.T) {
	svc, store := build(initialWallets())

	req := happyReq()
	req.Amount = 99999999

	resp, err := svc.CreateTransfer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "FAILED", resp.Status)
	require.NotNil(t, resp.FailureReason)
	assert.Equal(t, "INSUFFICIENT_FUNDS", *resp.FailureReason)

	assert.Equal(t, int64(10000), store.wallets["wallet_a"].Balance.Minor())
	assert.Equal(t, int64(0), store.wallets["wallet_b"].Balance.Minor())
	assert.Empty(t, store.ledger)
}

// S3 — Same wallet rejected before any DB call.
func TestS3_SameWallet(t *testing.T) {
	svc, store := build(initialWallets())

	req := happyReq()
	req.ToWalletID = req.FromWalletID

	_, err := svc.CreateTransfer(context.Background(), req)
	requireAppError(t, err, "SAME_WALLET")
	assert.Empty(t, store.transfers)
}

// S4 — Non-positive amount rejected.
func TestS4_InvalidAmount(t *testing.T) {
	svc, store := build(initialWallets())

	for _, amount := range []int64{0, -1, -100} {
		req := happyReq()
		req.Amount = amount
		_, err := svc.CreateTransfer(context.Background(), req)
		requireAppError(t, err, "INVALID_AMOUNT")
	}
	assert.Empty(t, store.transfers)
}

// S5 — Currency mismatch → FAILED.
func TestS5_CurrencyMismatch(t *testing.T) {
	wallets := []model.Wallet{
		{ID: "wallet_a", Balance: money.FromMinor(10000), Currency: "INR"},
		{ID: "wallet_b", Balance: money.FromMinor(0), Currency: "USD"},
	}
	svc, store := build(wallets)

	resp, err := svc.CreateTransfer(context.Background(), happyReq())
	require.NoError(t, err)
	assert.Equal(t, "FAILED", resp.Status)
	require.NotNil(t, resp.FailureReason)
	assert.Equal(t, "CURRENCY_MISMATCH", *resp.FailureReason)

	assert.Equal(t, int64(10000), store.wallets["wallet_a"].Balance.Minor())
	assert.Equal(t, int64(0), store.wallets["wallet_b"].Balance.Minor())
	assert.Empty(t, store.ledger)
}

// S6 — Wallet not found → 404.
func TestS6_WalletNotFound(t *testing.T) {
	svc, _ := build(initialWallets())

	req := happyReq()
	req.ToWalletID = "wallet_unknown"

	_, err := svc.CreateTransfer(context.Background(), req)
	requireAppError(t, err, "WALLET_NOT_FOUND")
}

// S7 — Idempotent replay (PROCESSED).
func TestS7_IdempotentReplay_Processed(t *testing.T) {
	svc, store := build(initialWallets())

	first, err := svc.CreateTransfer(context.Background(), happyReq())
	require.NoError(t, err)
	assert.False(t, first.Replayed)

	second, err := svc.CreateTransfer(context.Background(), happyReq())
	require.NoError(t, err)
	assert.True(t, second.Replayed)
	assert.Equal(t, first.ID, second.ID)
	assert.Equal(t, "PROCESSED", second.Status)

	assert.Equal(t, int64(9900), store.wallets["wallet_a"].Balance.Minor())
	assert.Equal(t, int64(100), store.wallets["wallet_b"].Balance.Minor())
	assert.Len(t, store.ledger, 2)
}

// S8 — Idempotent replay (FAILED).
func TestS8_IdempotentReplay_Failed(t *testing.T) {
	svc, store := build(initialWallets())

	req := happyReq()
	req.Amount = 99999999

	first, err := svc.CreateTransfer(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "FAILED", first.Status)

	second, err := svc.CreateTransfer(context.Background(), req)
	require.NoError(t, err)
	assert.True(t, second.Replayed)
	assert.Equal(t, "FAILED", second.Status)
	assert.Equal(t, first.ID, second.ID)

	assert.Equal(t, int64(10000), store.wallets["wallet_a"].Balance.Minor())
	assert.Empty(t, store.ledger)
}

// S9 — Same key, different body → 409.
func TestS9_IdempotencyConflict(t *testing.T) {
	svc, store := build(initialWallets())

	first, err := svc.CreateTransfer(context.Background(), happyReq())
	require.NoError(t, err)
	assert.False(t, first.Replayed)

	conflict := happyReq()
	conflict.Amount = 999

	_, err = svc.CreateTransfer(context.Background(), conflict)
	requireAppError(t, err, "IDEMPOTENCY_CONFLICT")

	assert.Equal(t, int64(9900), store.wallets["wallet_a"].Balance.Minor())
	assert.Equal(t, int64(100), store.wallets["wallet_b"].Balance.Minor())
	assert.Len(t, store.ledger, 2)
}

// -----------------------------------------------------------------------------
// Test helpers
// -----------------------------------------------------------------------------

func requireAppError(t *testing.T, err error, code string) {
	t.Helper()
	require.Error(t, err)
	ae, ok := apperr.As(err)
	require.True(t, ok, "expected AppError, got %T: %v", err, err)
	assert.Equal(t, code, ae.Code, "AppError.Code mismatch (msg=%s)", ae.Message)
}
