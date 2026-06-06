package service_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

// ── Test doubles ─────────────────────────────────────────────────────────────

// fakeTx is a minimal pgx.Tx test double. All operations are no-ops; tests
// override Commit/Rollback behaviour via callbacks.
type fakeTx struct {
	pgx.Tx
	committed  bool
	rolledBack bool
}

func (f *fakeTx) Commit(_ context.Context) error                         { f.committed = true; return nil }
func (f *fakeTx) Rollback(_ context.Context) error                       { f.rolledBack = true; return nil }
func (f *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return nil }
func (f *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

// fakeDB is a test double for the pgxpool that can begin transactions.
type fakeDB struct {
	tx *fakeTx
}

func (db *fakeDB) Begin(_ context.Context) (pgx.Tx, error) {
	db.tx = &fakeTx{}

	return db.tx, nil
}

func (db *fakeDB) BeginTx(_ context.Context, _ pgx.TxOptions) (pgx.Tx, error) {
	db.tx = &fakeTx{}

	return db.tx, nil
}

func (db *fakeDB) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row { return nil }
func (db *fakeDB) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}

// fakeWalletRepo lets individual tests control what each method returns.
type fakeWalletRepo struct {
	wallets      map[uuid.UUID]domain.Wallet
	createErr    error
	updateErr    error
	getForUpdate func(id uuid.UUID) (domain.Wallet, error)
}

func (r *fakeWalletRepo) Create(_ context.Context, _ repository.Querier, w domain.Wallet) error {
	if r.createErr != nil {
		return r.createErr
	}

	if r.wallets == nil {
		r.wallets = make(map[uuid.UUID]domain.Wallet)
	}

	r.wallets[w.ID] = w

	return nil
}

func (r *fakeWalletRepo) GetByID(_ context.Context, _ repository.Querier, id uuid.UUID) (domain.Wallet, error) {
	if w, ok := r.wallets[id]; ok {
		return w, nil
	}

	return domain.Wallet{}, domain.ErrWalletNotFound
}

func (r *fakeWalletRepo) GetByIDForUpdate(_ context.Context, _ pgx.Tx, id uuid.UUID) (domain.Wallet, error) {
	if r.getForUpdate != nil {
		return r.getForUpdate(id)
	}

	return r.GetByID(context.Background(), nil, id)
}

func (r *fakeWalletRepo) UpdateBalance(_ context.Context, _ pgx.Tx, id uuid.UUID, newBalance domain.Amount) error {
	if r.updateErr != nil {
		return r.updateErr
	}

	if w, ok := r.wallets[id]; ok {
		w.Balance = newBalance
		r.wallets[id] = w
	}

	return nil
}

// fakeTransferRepo tracks created transfers and allows UpdateStatus override.
type fakeTransferRepo struct {
	transfers map[uuid.UUID]domain.Transfer
	createErr error
	updateErr error
}

func (r *fakeTransferRepo) Create(_ context.Context, _ repository.Querier, t domain.Transfer) error {
	if r.createErr != nil {
		return r.createErr
	}

	if r.transfers == nil {
		r.transfers = make(map[uuid.UUID]domain.Transfer)
	}

	r.transfers[t.ID] = t

	return nil
}

func (r *fakeTransferRepo) GetByID(_ context.Context, _ repository.Querier, id uuid.UUID) (domain.Transfer, error) {
	if t, ok := r.transfers[id]; ok {
		return t, nil
	}

	return domain.Transfer{}, domain.ErrTransferNotFound
}

func (r *fakeTransferRepo) UpdateStatus(_ context.Context, _ pgx.Tx, id uuid.UUID, status domain.TransferStatus) error {
	if r.updateErr != nil {
		return r.updateErr
	}

	if t, ok := r.transfers[id]; ok {
		t.Status = status
		r.transfers[id] = t
	}

	return nil
}

// fakeLedgerRepo records the entries it received.
type fakeLedgerRepo struct {
	entries []domain.LedgerEntry
}

func (r *fakeLedgerRepo) CreateEntries(_ context.Context, _ pgx.Tx, debit, credit domain.LedgerEntry) error {
	r.entries = append(r.entries, debit, credit)

	return nil
}

// fakeIdempotencyRepo stores records in memory.
type fakeIdempotencyRepo struct {
	records map[string]repository.IdempotencyRecord
}

func (r *fakeIdempotencyRepo) Get(_ context.Context, _ repository.Querier, key string) (repository.IdempotencyRecord, error) {
	if rec, ok := r.records[key]; ok {
		return rec, nil
	}

	return repository.IdempotencyRecord{}, domain.ErrIdempotencyKeyNotFound
}

func (r *fakeIdempotencyRepo) Create(_ context.Context, _ pgx.Tx, rec repository.IdempotencyRecord) error {
	if r.records == nil {
		r.records = make(map[string]repository.IdempotencyRecord)
	}

	r.records[rec.Key] = rec

	return nil
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func newServiceWithFakes() (
	*service.TransferService,
	*fakeDB,
	*fakeWalletRepo,
	*fakeTransferRepo,
	*fakeLedgerRepo,
	*fakeIdempotencyRepo,
) {
	db := &fakeDB{}
	wallets := &fakeWalletRepo{}
	transfers := &fakeTransferRepo{}
	ledger := &fakeLedgerRepo{}
	idem := &fakeIdempotencyRepo{}

	// Discard logs during tests.
	svc := service.NewTransferService(db, wallets, transfers, ledger, idem,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))

	return svc, db, wallets, transfers, ledger, idem
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// ── CreateWallet ──────────────────────────────────────────────────────────────

func TestTransferService_CreateWallet_HappyPath(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	w, err := svc.CreateWallet(ctx)
	if err != nil {
		t.Fatalf("CreateWallet: %v", err)
	}

	// ID must be non-zero.
	if w.ID == (uuid.UUID{}) {
		t.Fatal("created wallet must have a non-zero ID")
	}

	// Balance must be zero on creation.
	if w.Balance != 0 {
		t.Fatalf("expected zero balance, got %d", w.Balance)
	}

	// Wallet must be persisted in the repo.
	stored, err := wallets.GetByID(ctx, nil, w.ID)
	if err != nil {
		t.Fatalf("wallet not found in repo after create: %v", err)
	}

	if stored.ID != w.ID {
		t.Fatalf("stored wallet ID mismatch: got %s, want %s", stored.ID, w.ID)
	}
}

func TestTransferService_CreateWallet_RepoError(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	wallets.createErr = errors.New("db: disk full")

	_, err := svc.CreateWallet(ctx)
	if err == nil {
		t.Fatal("expected error when repo Create fails, got nil")
	}
}

func TestTransferService_Execute_HappyPath(t *testing.T) {
	t.Parallel()

	svc, _, wallets, transferRepo, ledger, idem := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "happy-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00,
	}

	resp, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Transfer must be PROCESSED.
	if resp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", resp.Status)
	}

	// Exactly two ledger entries.
	if len(ledger.entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(ledger.entries))
	}

	var debitEntry, creditEntry domain.LedgerEntry
	for _, e := range ledger.entries {
		switch e.Type {
		case domain.EntryTypeDebit:
			debitEntry = e
		case domain.EntryTypeCredit:
			creditEntry = e
		}
	}

	if debitEntry.WalletID != from.ID {
		t.Fatal("debit must be from source wallet")
	}

	if creditEntry.WalletID != to.ID {
		t.Fatal("credit must be to destination wallet")
	}

	if debitEntry.Amount != req.Amount || creditEntry.Amount != req.Amount {
		t.Fatal("ledger entry amounts must match the transfer amount")
	}

	// Idempotency record must be stored.
	if _, err := idem.Get(ctx, nil, req.IdempotencyKey); err != nil {
		t.Fatal("idempotency record must be stored after successful transfer")
	}

	// Transfer must exist in repo.
	tr, err := transferRepo.GetByID(ctx, nil, resp.TransferID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}

	if tr.Status != domain.TransferStatusProcessed {
		t.Fatalf("stored transfer status: got %s, want PROCESSED", tr.Status)
	}
}

func TestTransferService_Execute_InsufficientFunds(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 50_00 // only 50
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "insuf-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00, // requesting 100
	}

	_, err := svc.Execute(ctx, req)
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}
}

func TestTransferService_Execute_Idempotent(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "idem-svc-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00,
	}

	// First call — should process.
	resp1, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	// Second call with same key — must return identical response without re-running.
	resp2, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("second Execute: %v", err)
	}

	if resp1.TransferID != resp2.TransferID {
		t.Fatal("idempotent repeat must return the same transfer ID")
	}
}

func TestTransferService_Execute_SameWallet(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	w := domain.NewWallet()
	w.Balance = 500_00

	wallets.wallets = map[uuid.UUID]domain.Wallet{w.ID: w}

	req := service.TransferRequest{
		IdempotencyKey: "same-wallet-001",
		FromWalletID:   w.ID,
		ToWalletID:     w.ID, // same!
		Amount:         100_00,
	}

	_, err := svc.Execute(ctx, req)
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("expected ErrSameWallet, got %v", err)
	}
}

func TestTransferService_Execute_InvalidAmount(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	cases := []domain.Amount{0, -1}

	for _, amount := range cases {
		req := service.TransferRequest{
			IdempotencyKey: "invalid-amount",
			FromWalletID:   from.ID,
			ToWalletID:     to.ID,
			Amount:         amount,
		}

		_, err := svc.Execute(ctx, req)
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Fatalf("amount %d: expected ErrInvalidAmount, got %v", amount, err)
		}
	}
}

func TestTransferService_Execute_WalletNotFound(t *testing.T) {
	t.Parallel()

	svc, _, _, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "notfound-001",
		FromWalletID:   domain.NewID(),
		ToWalletID:     domain.NewID(),
		Amount:         100_00,
	}

	_, err := svc.Execute(ctx, req)
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

func TestTransferService_Execute_IdempotentResponse_JSON(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, idem := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	req := service.TransferRequest{
		IdempotencyKey: "json-idem-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         50_00,
	}

	// First call to populate the idempotency cache.
	resp1, err := svc.Execute(ctx, req)
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	// Verify the cached JSON decodes back to the same response.
	rec, err := idem.Get(ctx, nil, req.IdempotencyKey)
	if err != nil {
		t.Fatalf("Get idempotency record: %v", err)
	}

	var decoded service.TransferResponse
	if err := json.Unmarshal([]byte(rec.ResponseJSON), &decoded); err != nil {
		t.Fatalf("unmarshal cached JSON: %v", err)
	}

	if decoded.TransferID != resp1.TransferID {
		t.Fatal("cached JSON transfer ID must match original response")
	}
}

// TestTransferService_LedgerInvariant verifies that each successful transfer
// produces exactly one DEBIT and one CREDIT entry with equal amounts, and that
// the total balances across both wallets are conserved.
func TestTransferService_LedgerInvariant(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, ledger, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 300_00
	to := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	const transferAmount domain.Amount = 75_00

	_, err := svc.Execute(ctx, service.TransferRequest{
		IdempotencyKey: "ledger-inv-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         transferAmount,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// Exactly two entries must be produced.
	if len(ledger.entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(ledger.entries))
	}

	var (
		totalDebit  domain.Amount
		totalCredit domain.Amount
	)

	for _, e := range ledger.entries {
		switch e.Type {
		case domain.EntryTypeDebit:
			totalDebit += e.Amount
		case domain.EntryTypeCredit:
			totalCredit += e.Amount
		}
	}

	// Debit == Credit == transfer amount (double-entry invariant).
	if totalDebit != transferAmount {
		t.Fatalf("debit total %d != transfer amount %d", totalDebit, transferAmount)
	}

	if totalCredit != transferAmount {
		t.Fatalf("credit total %d != transfer amount %d", totalCredit, transferAmount)
	}

	// Balance conservation: new_from + new_to == original_from + original_to.
	newFrom := wallets.wallets[from.ID].Balance
	newTo := wallets.wallets[to.ID].Balance

	if newFrom+newTo != from.Balance+to.Balance {
		t.Fatalf("balance conservation violated: %d + %d != %d + %d",
			newFrom, newTo, from.Balance, to.Balance)
	}

	// Spot-check individual balances.
	if newFrom != from.Balance-transferAmount {
		t.Fatalf("sender balance: got %d, want %d", newFrom, from.Balance-transferAmount)
	}

	if newTo != to.Balance+transferAmount {
		t.Fatalf("receiver balance: got %d, want %d", newTo, to.Balance+transferAmount)
	}
}

// TestTransferService_UpdateBalanceFailure asserts that a failure mid-transaction
// (e.g. UpdateBalance DB error) causes Execute to return an error and not store
// an idempotency record — leaving the system as if no transfer occurred.
func TestTransferService_UpdateBalanceFailure(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, idem := newServiceWithFakes()
	ctx := context.Background()

	dbErr := errors.New("db: disk full")

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	// Inject a DB error on UpdateBalance.
	wallets.updateErr = dbErr

	req := service.TransferRequest{
		IdempotencyKey: "update-fail-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00,
	}

	_, err := svc.Execute(ctx, req)
	if err == nil {
		t.Fatal("expected error when UpdateBalance fails, got nil")
	}

	// No idempotency record must have been stored.
	if _, getErr := idem.Get(ctx, nil, req.IdempotencyKey); getErr == nil {
		t.Fatal("idempotency record must NOT be stored when transfer fails")
	}

	// Sender balance must be unchanged.
	if wallets.wallets[from.ID].Balance != from.Balance {
		t.Fatal("sender balance must be unchanged on failure")
	}
}

// TestTransferService_CreateTransferFailure checks that a Create error on the
// transfer repo (e.g. constraint violation) propagates correctly and leaves no
// idempotency record behind.
func TestTransferService_CreateTransferFailure(t *testing.T) {
	t.Parallel()

	svc, _, wallets, transferRepo, _, idem := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	// Force Create to fail.
	transferRepo.createErr = errors.New("db: unexpected failure")

	req := service.TransferRequest{
		IdempotencyKey: "create-fail-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         50_00,
	}

	_, err := svc.Execute(ctx, req)
	if err == nil {
		t.Fatal("expected error when transfer Create fails, got nil")
	}

	// No idempotency record must have been stored.
	if _, getErr := idem.Get(ctx, nil, req.IdempotencyKey); getErr == nil {
		t.Fatal("idempotency record must NOT be stored when transfer creation fails")
	}
}

// TestTransferService_Execute_ConflictingIdempotencyKey asserts that reusing
// an idempotency key with different request parameters returns
// domain.ErrDuplicateIdempotencyKey rather than silently replaying the
// original cached response.
func TestTransferService_Execute_ConflictingIdempotencyKey(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()
	other := domain.NewWallet()

	wallets.wallets = map[uuid.UUID]domain.Wallet{
		from.ID:  from,
		to.ID:    to,
		other.ID: other,
	}

	const key = "conflict-key-001"

	// First request — should succeed.
	_, err := svc.Execute(ctx, service.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         100_00,
	})
	if err != nil {
		t.Fatalf("first Execute: %v", err)
	}

	// Second request with the same key but different parameters — must be rejected.
	_, err = svc.Execute(ctx, service.TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   from.ID,
		ToWalletID:     other.ID, // different destination
		Amount:         100_00,
	})
	if !errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		t.Fatalf("expected ErrDuplicateIdempotencyKey for conflicting key reuse, got: %v", err)
	}
}

// TestTransferService_Execute_StatusInCachedJSON verifies that the cached
// TransferResponse JSON uses the human-readable status string ("PROCESSED"),
// not an integer, so that it round-trips correctly through json.Unmarshal.
func TestTransferService_Execute_StatusInCachedJSON(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, idem := newServiceWithFakes()
	ctx := context.Background()

	from := domain.NewWallet()
	from.Balance = 500_00
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{from.ID: from, to.ID: to}

	_, err := svc.Execute(ctx, service.TransferRequest{
		IdempotencyKey: "json-status-001",
		FromWalletID:   from.ID,
		ToWalletID:     to.ID,
		Amount:         50_00,
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec, err := idem.Get(ctx, nil, "json-status-001")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	// Status should be the string "PROCESSED", not the integer 2.
	if !strings.Contains(rec.ResponseJSON, `"PROCESSED"`) {
		t.Fatalf("cached JSON should contain human-readable status, got: %s", rec.ResponseJSON)
	}
}

// ── Magic ─────────────────────────────────────────────────────────────────────

// TestTransferService_Magic_HappyPath verifies that Magic deposits a random
// amount in the expected range and returns a PROCESSED transfer.
func TestTransferService_Magic_HappyPath(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, ledger, _ := newServiceWithFakes()
	ctx := context.Background()

	// Seed the nature wallet with ample funds.
	nature := domain.Wallet{ID: domain.NatureWalletID, Balance: 1_000_000}
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{nature.ID: nature, to.ID: to}

	resp, err := svc.Magic(ctx, service.MagicRequest{
		IdempotencyKey: "magic-001",
		ToWalletID:     to.ID,
	})
	if err != nil {
		t.Fatalf("Magic: %v", err)
	}

	if resp.Status != domain.TransferStatusProcessed {
		t.Fatalf("expected PROCESSED, got %s", resp.Status)
	}

	// Amount must be within the allowed range [100, 10 000].
	if resp.Amount < 100 || resp.Amount > 10_000 {
		t.Fatalf("amount %d outside expected range [100, 10000]", resp.Amount)
	}

	// Exactly two ledger entries must be created.
	if len(ledger.entries) != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", len(ledger.entries))
	}
}

// TestTransferService_Magic_Idempotent verifies that repeating a Magic call
// with the same idempotency key returns an identical response.
func TestTransferService_Magic_Idempotent(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, _, _ := newServiceWithFakes()
	ctx := context.Background()

	nature := domain.Wallet{ID: domain.NatureWalletID, Balance: 1_000_000}
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{nature.ID: nature, to.ID: to}

	req := service.MagicRequest{
		IdempotencyKey: "magic-idem-001",
		ToWalletID:     to.ID,
	}

	resp1, err := svc.Magic(ctx, req)
	if err != nil {
		t.Fatalf("first Magic: %v", err)
	}

	resp2, err := svc.Magic(ctx, req)
	if err != nil {
		t.Fatalf("second Magic: %v", err)
	}

	if resp1.TransferID != resp2.TransferID {
		t.Fatal("idempotent repeat must return the same transfer ID")
	}

	if resp1.Amount != resp2.Amount {
		t.Fatal("idempotent repeat must return the same amount")
	}
}

// TestTransferService_Magic_FromNatureWallet verifies that the debit ledger
// entry is always charged to domain.NatureWalletID, not some other wallet.
func TestTransferService_Magic_FromNatureWallet(t *testing.T) {
	t.Parallel()

	svc, _, wallets, _, ledger, _ := newServiceWithFakes()
	ctx := context.Background()

	nature := domain.Wallet{ID: domain.NatureWalletID, Balance: 1_000_000}
	to := domain.NewWallet()
	wallets.wallets = map[uuid.UUID]domain.Wallet{nature.ID: nature, to.ID: to}

	if _, err := svc.Magic(ctx, service.MagicRequest{
		IdempotencyKey: "magic-from-001",
		ToWalletID:     to.ID,
	}); err != nil {
		t.Fatalf("Magic: %v", err)
	}

	for _, e := range ledger.entries {
		if e.Type == domain.EntryTypeDebit && e.WalletID != domain.NatureWalletID {
			t.Fatalf("debit entry wallet %s must equal NatureWalletID", e.WalletID)
		}
	}
}
