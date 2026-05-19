package service_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/repository"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/service"
)

// --- in-memory fakes (no DB; the db handle is unused) ---

type fakeTx struct{}

func (fakeTx) WithTx(_ context.Context, fn func(repository.DBTX) error) error { return fn(nil) }
func (fakeTx) DB() repository.DBTX                                            { return nil }

type fakeWalletStore struct{ w map[string]domain.Wallet }

func newFakeWalletStore() *fakeWalletStore { return &fakeWalletStore{w: map[string]domain.Wallet{}} }

func (f *fakeWalletStore) seed(id, balance string) {
	f.w[id] = domain.Wallet{ID: id, Name: id, Balance: decimal.RequireFromString(balance)}
}

func (f *fakeWalletStore) GetForUpdate(_ context.Context, _ repository.DBTX, id string) (domain.Wallet, error) {
	if x, ok := f.w[id]; ok {
		return x, nil
	}
	return domain.Wallet{}, domain.ErrWalletNotFound
}

func (f *fakeWalletStore) AddToBalance(_ context.Context, _ repository.DBTX, id string, delta decimal.Decimal) error {
	x, ok := f.w[id]
	if !ok {
		return domain.ErrWalletNotFound
	}
	x.Balance = x.Balance.Add(delta)
	f.w[id] = x
	return nil
}

type fakeTransferStore struct {
	byID  map[string]domain.Transfer
	byKey map[string]string
	seq   int
}

func newFakeTransferStore() *fakeTransferStore {
	return &fakeTransferStore{byID: map[string]domain.Transfer{}, byKey: map[string]string{}}
}

func (f *fakeTransferStore) InsertPending(_ context.Context, _ repository.DBTX, t domain.Transfer) (domain.Transfer, error) {
	if _, ok := f.byKey[t.IdempotencyKey]; ok {
		return domain.Transfer{}, domain.ErrDuplicateIdempotencyKey
	}
	f.seq++
	t.ID = fmt.Sprintf("tr-%d", f.seq)
	t.Status = domain.TransferPending
	f.byID[t.ID] = t
	f.byKey[t.IdempotencyKey] = t.ID
	return t, nil
}

func (f *fakeTransferStore) GetByIdempotencyKey(_ context.Context, _ repository.DBTX, key string) (domain.Transfer, error) {
	if id, ok := f.byKey[key]; ok {
		return f.byID[id], nil
	}
	return domain.Transfer{}, domain.ErrTransferNotFound
}

func (f *fakeTransferStore) GetByID(_ context.Context, _ repository.DBTX, id string) (domain.Transfer, error) {
	if t, ok := f.byID[id]; ok {
		return t, nil
	}
	return domain.Transfer{}, domain.ErrTransferNotFound
}

func (f *fakeTransferStore) UpdateStatus(_ context.Context, _ repository.DBTX, id string, status domain.TransferStatus, reason string) error {
	t, ok := f.byID[id]
	if !ok {
		return domain.ErrTransferNotFound
	}
	t.Status = status
	t.FailureReason = reason
	f.byID[id] = t
	return nil
}

type fakeLedgerStore struct {
	entries []domain.LedgerEntry
	seen    map[string]bool
}

func newFakeLedgerStore() *fakeLedgerStore {
	return &fakeLedgerStore{seen: map[string]bool{}}
}

func (f *fakeLedgerStore) InsertEntry(_ context.Context, _ repository.DBTX, e domain.LedgerEntry) error {
	k := e.TransferID + "|" + string(e.Type)
	if f.seen[k] {
		return errors.New("duplicate ledger entry")
	}
	f.seen[k] = true
	f.entries = append(f.entries, e)
	return nil
}

// --- helpers ---

func newSvc(t *testing.T) (*service.TransferService, *fakeWalletStore, *fakeTransferStore, *fakeLedgerStore) {
	t.Helper()
	w := newFakeWalletStore()
	tr := newFakeTransferStore()
	l := newFakeLedgerStore()
	return service.NewTransferService(fakeTx{}, w, tr, l), w, tr, l
}

func bal(t *testing.T, w *fakeWalletStore, id string) decimal.Decimal {
	t.Helper()
	return w.w[id].Balance
}

// --- tests ---

func TestCreateTransfer_HappyPath(t *testing.T) {
	svc, w, _, l := newSvc(t)
	w.seed("w1", "100.00")
	w.seed("w2", "0.00")

	got, created, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2",
		Amount: decimal.RequireFromString("30.00"),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !created {
		t.Fatal("created = false, want true for a new transfer")
	}
	if got.Status != domain.TransferProcessed {
		t.Fatalf("status = %s, want PROCESSED", got.Status)
	}
	if !bal(t, w, "w1").Equal(decimal.RequireFromString("70.00")) ||
		!bal(t, w, "w2").Equal(decimal.RequireFromString("30.00")) {
		t.Fatalf("balances w1=%s w2=%s, want 70.00 / 30.00", bal(t, w, "w1"), bal(t, w, "w2"))
	}
	if len(l.entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(l.entries))
	}
	var debit, credit domain.LedgerEntry
	for _, e := range l.entries {
		switch e.Type {
		case domain.LedgerDebit:
			debit = e
		case domain.LedgerCredit:
			credit = e
		}
	}
	if debit.WalletID != "w1" || credit.WalletID != "w2" ||
		!debit.Amount.Equal(decimal.RequireFromString("30.00")) ||
		!credit.Amount.Equal(decimal.RequireFromString("30.00")) {
		t.Fatalf("bad double entry: debit=%+v credit=%+v", debit, credit)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, w, _, l := newSvc(t)
	w.seed("w1", "10.00")
	w.seed("w2", "0.00")

	got, created, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2",
		Amount: decimal.RequireFromString("50.00"),
	})
	if err != nil {
		t.Fatalf("insufficient funds must not be an error, got %v", err)
	}
	if !created {
		t.Fatal("created = false, want true (a FAILED transfer row was still created)")
	}
	if got.Status != domain.TransferFailed || got.FailureReason != "insufficient funds" {
		t.Fatalf("got status=%s reason=%q, want FAILED / insufficient funds",
			got.Status, got.FailureReason)
	}
	if !bal(t, w, "w1").Equal(decimal.RequireFromString("10.00")) ||
		!bal(t, w, "w2").Equal(decimal.RequireFromString("0.00")) {
		t.Fatalf("balances changed on failure: w1=%s w2=%s", bal(t, w, "w1"), bal(t, w, "w2"))
	}
	if len(l.entries) != 0 {
		t.Fatalf("ledger entries = %d, want 0 on failure", len(l.entries))
	}
}

func TestCreateTransfer_Validation(t *testing.T) {
	svc, w, _, _ := newSvc(t)
	w.seed("w1", "100.00")

	_, _, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w1",
		Amount: decimal.RequireFromString("10.00"),
	})
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Fatalf("got %v, want ErrSameWallet", err)
	}

	_, _, err = svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: "k2", FromWalletID: "w1", ToWalletID: "w2",
		Amount: decimal.Zero,
	})
	if !errors.Is(err, domain.ErrInvalidAmount) {
		t.Fatalf("got %v, want ErrInvalidAmount", err)
	}
}

func TestCreateTransfer_WalletNotFound(t *testing.T) {
	svc, w, tr, _ := newSvc(t)
	w.seed("w2", "0.00") // only destination exists

	_, _, err := svc.CreateTransfer(context.Background(), service.CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2",
		Amount: decimal.RequireFromString("10.00"),
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("got %v, want ErrWalletNotFound", err)
	}
	if len(tr.byID) != 0 {
		t.Fatalf("a transfer row was created on 404: %d rows", len(tr.byID))
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	svc, w, tr, l := newSvc(t)
	w.seed("w1", "100.00")
	w.seed("w2", "0.00")
	in := service.CreateTransferInput{
		IdempotencyKey: "k1", FromWalletID: "w1", ToWalletID: "w2",
		Amount: decimal.RequireFromString("40.00"),
	}

	first, c1, err := svc.CreateTransfer(context.Background(), in)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, c2, err := svc.CreateTransfer(context.Background(), in)
	if err != nil {
		t.Fatalf("second: %v", err)
	}

	if !c1 || c2 {
		t.Fatalf("created flags: first=%v second=%v, want true/false", c1, c2)
	}
	if first.ID != second.ID {
		t.Fatalf("replay returned different id: %s != %s", first.ID, second.ID)
	}
	if len(tr.byID) != 1 {
		t.Fatalf("transfer rows = %d, want 1", len(tr.byID))
	}
	if len(l.entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2 (side effects applied once)", len(l.entries))
	}
	if !bal(t, w, "w1").Equal(decimal.RequireFromString("60.00")) ||
		!bal(t, w, "w2").Equal(decimal.RequireFromString("40.00")) {
		t.Fatalf("balances applied more than once: w1=%s w2=%s", bal(t, w, "w1"), bal(t, w, "w2"))
	}
}
