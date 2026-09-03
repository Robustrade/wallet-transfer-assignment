package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/database"
	"github.com/Robustrade/wallet-transfer-assignment/internal/dbtest"
	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
)

type fixture struct {
	t     *testing.T
	db    *database.DB
	store *repository.Store
	svc   *service.Service
}

func newFixture(t *testing.T, db *database.DB) *fixture {
	store := repository.NewStore(db)
	return &fixture{t: t, db: db, store: store, svc: service.New(store)}
}

func (f *fixture) wallet(id string, balance int64) *domain.Wallet {
	f.t.Helper()
	w, err := f.svc.CreateWallet(context.Background(), id, balance)
	if err != nil {
		f.t.Fatalf("create wallet %s: %v", id, err)
	}
	return w
}

func (f *fixture) balance(id string) int64 {
	f.t.Helper()
	w, err := f.svc.GetWallet(context.Background(), id)
	if err != nil {
		f.t.Fatalf("get wallet %s: %v", id, err)
	}
	return w.Balance
}

func (f *fixture) transfer(key, from, to string, amount int64) (*service.TransferResult, error) {
	return f.svc.CreateTransfer(context.Background(), domain.TransferRequest{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to, Amount: amount,
	})
}

func TestTransferSucceeds(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 500)
		f.wallet("bob", 100)

		result, err := f.transfer("key-1", "alice", "bob", 300)
		if err != nil {
			t.Fatalf("transfer: %v", err)
		}

		if result.Transfer.Status != domain.StatusProcessed {
			t.Fatalf("status = %s, want PROCESSED", result.Transfer.Status)
		}
		if result.Replayed {
			t.Error("first request reported as a replay")
		}
		if got := f.balance("alice"); got != 200 {
			t.Errorf("alice balance = %d, want 200", got)
		}
		if got := f.balance("bob"); got != 400 {
			t.Errorf("bob balance = %d, want 400", got)
		}
		assertLedgerPair(t, result, "alice", "bob", 300)
	})
}

func TestTransferRejectsInsufficientFunds(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 50)
		f.wallet("bob", 0)

		result, err := f.transfer("key-1", "alice", "bob", 100)
		if err != nil {
			t.Fatalf("transfer: %v", err)
		}

		if result.Transfer.Status != domain.StatusFailed {
			t.Fatalf("status = %s, want FAILED", result.Transfer.Status)
		}
		if result.Transfer.FailureReason != domain.FailureInsufficientFunds {
			t.Errorf("failure reason = %q, want %q", result.Transfer.FailureReason, domain.FailureInsufficientFunds)
		}
		if len(result.Entries) != 0 {
			t.Errorf("failed transfer wrote %d ledger entries, want 0", len(result.Entries))
		}
		if got := f.balance("alice"); got != 50 {
			t.Errorf("alice balance = %d, want 50", got)
		}
		if got := f.balance("bob"); got != 0 {
			t.Errorf("bob balance = %d, want 0", got)
		}
	})
}

func TestTransferRejectsInvalidRequests(t *testing.T) {
	dbtest.Run(t, func(t *testing.T, db *database.DB) {
		f := newFixture(t, db)
		f.wallet("alice", 500)
		f.wallet("bob", 500)

		cases := []struct {
			name string
			key  string
			from string
			to   string
			amt  int64
			want error
		}{
			{"unknown source", "k1", "ghost", "bob", 10, domain.ErrWalletNotFound},
			{"unknown destination", "k2", "alice", "ghost", 10, domain.ErrWalletNotFound},
			{"zero amount", "k3", "alice", "bob", 0, domain.ErrInvalidAmount},
			{"negative amount", "k4", "alice", "bob", -10, domain.ErrInvalidAmount},
			{"same wallet", "k5", "alice", "alice", 10, domain.ErrSameWallet},
			{"missing key", "", "alice", "bob", 10, domain.ErrMissingField},
			{"missing source", "k6", "", "bob", 10, domain.ErrMissingField},
			{"missing destination", "k7", "alice", "", 10, domain.ErrMissingField},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				if _, err := f.transfer(tc.key, tc.from, tc.to, tc.amt); !errors.Is(err, tc.want) {
					t.Fatalf("error = %v, want %v", err, tc.want)
				}
			})
		}

		if got := f.balance("alice"); got != 500 {
			t.Errorf("alice balance = %d, want 500", got)
		}
		if got := f.balance("bob"); got != 500 {
			t.Errorf("bob balance = %d, want 500", got)
		}
		if got := countRows(t, db, "transfers"); got != 0 {
			t.Errorf("transfers table has %d rows, want 0", got)
		}
	})
}

func assertLedgerPair(t *testing.T, result *service.TransferResult, from, to string, amount int64) {
	t.Helper()

	if len(result.Entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(result.Entries))
	}

	var debits, credits int64
	for _, e := range result.Entries {
		switch e.EntryType {
		case domain.EntryDebit:
			debits += e.Amount
			if e.WalletID != from {
				t.Errorf("debit wallet = %s, want %s", e.WalletID, from)
			}
		case domain.EntryCredit:
			credits += e.Amount
			if e.WalletID != to {
				t.Errorf("credit wallet = %s, want %s", e.WalletID, to)
			}
		}
		if e.TransferID != result.Transfer.ID {
			t.Errorf("entry belongs to transfer %s, want %s", e.TransferID, result.Transfer.ID)
		}
	}
	if debits != amount || credits != amount {
		t.Errorf("debits = %d, credits = %d, want %d each", debits, credits, amount)
	}
}
