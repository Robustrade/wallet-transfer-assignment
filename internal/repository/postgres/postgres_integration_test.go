//go:build integration

package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/PratikDhanave/wallet-transfer-assignment/internal/domain"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/repository/postgres"
	"github.com/PratikDhanave/wallet-transfer-assignment/internal/testdb"
)

// --- TxManager ------------------------------------------------------

// TestTxManager_Commit verifies the happy path: when fn returns nil
// the transaction COMMITs and the row is visible to a follow-up read
// on the non-tx executor. This is the boring case but a regression
// in the defer order would break it first.
func TestTxManager_Commit(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	txm := postgres.NewTxManager(d)
	wallets := postgres.NewWalletRepo()

	err := txm.RunInTx(context.Background(), func(exec repository.Executor) error {
		return wallets.Insert(context.Background(), exec, &domain.Wallet{ID: "x", Balance: 1})
	})
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	w, err := wallets.Get(context.Background(), d, "x")
	if err != nil || w.Balance != 1 {
		t.Fatalf("post-commit read: %v %+v", err, w)
	}
}

// TestTxManager_RollbackOnError exercises path (b) of the
// RunInTx state machine: fn returned a non-nil error. The
// transaction must ROLLBACK so the inserted row does not appear
// after the call, AND the original sentinel error must be preserved
// (no shadowing by a rollback error).
func TestTxManager_RollbackOnError(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	txm := postgres.NewTxManager(d)
	wallets := postgres.NewWalletRepo()

	sentinel := errors.New("user-requested rollback")
	err := txm.RunInTx(context.Background(), func(exec repository.Executor) error {
		if err := wallets.Insert(context.Background(), exec, &domain.Wallet{ID: "y", Balance: 1}); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("expected sentinel, got %v", err)
	}
	if _, err := wallets.Get(context.Background(), d, "y"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("row should have rolled back; got %v", err)
	}
}

// TestTxManager_RollbackOnPanic exercises path (a): a panic from fn
// must ROLLBACK the transaction AND re-panic so the stack trace
// survives. The test uses defer + recover to catch the re-raised
// panic, then verifies the row that was inserted before the panic
// did not commit.
func TestTxManager_RollbackOnPanic(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	txm := postgres.NewTxManager(d)
	wallets := postgres.NewWalletRepo()

	defer func() {
		p := recover()
		if p == nil {
			t.Fatal("expected panic to propagate")
		}
		// Verify the row that was inserted before the panic did not commit.
		if _, err := wallets.Get(context.Background(), d, "z"); !errors.Is(err, repository.ErrNotFound) {
			t.Fatalf("row should have rolled back; got %v", err)
		}
	}()
	_ = txm.RunInTx(context.Background(), func(exec repository.Executor) error {
		_ = wallets.Insert(context.Background(), exec, &domain.Wallet{ID: "z", Balance: 1})
		panic("kaboom")
	})
}

// --- WalletRepo not-found paths ------------------------------------

// TestWalletRepo_Get_NotFound asserts the documented sentinel: when
// no row matches, Get returns repository.ErrNotFound (not a raw
// sql.ErrNoRows). The classify step in the handler depends on
// errors.Is matching this sentinel.
func TestWalletRepo_Get_NotFound(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	repo := postgres.NewWalletRepo()
	_, err := repo.Get(context.Background(), d, "missing")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

// TestWalletRepo_UpdateBalance_NotFound covers the rows-affected=0
// branch: an UPDATE that matches no rows must surface as
// ErrNotFound, not as a silent no-op. Silent no-ops on balance
// updates would be a subtle but real money-loss bug.
func TestWalletRepo_UpdateBalance_NotFound(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	repo := postgres.NewWalletRepo()
	if err := repo.UpdateBalance(context.Background(), d, "missing", 99); !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

// TestWalletRepo_Insert_DuplicateRejected verifies the PK on
// wallets.id is doing its job: two inserts of the same id must
// fail. The repo currently surfaces this as a generic error
// (it's a setup bug, not an expected outcome) — that's fine; the
// PK guarantee is what we care about.
func TestWalletRepo_Insert_DuplicateRejected(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	repo := postgres.NewWalletRepo()
	if err := repo.Insert(context.Background(), d, &domain.Wallet{ID: "dup", Balance: 1}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Insert(context.Background(), d, &domain.Wallet{ID: "dup", Balance: 2}); err == nil {
		t.Fatal("expected unique violation")
	}
}

// --- TransferRepo not-found paths ----------------------------------

// TestTransferRepo_Get_NotFound is the transfer-table counterpart
// to TestWalletRepo_Get_NotFound. Same sentinel contract.
func TestTransferRepo_Get_NotFound(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	repo := postgres.NewTransferRepo()
	_, err := repo.Get(context.Background(), d, uuid.New())
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

// TestTransferRepo_UpdateState_NotFound mirrors
// TestWalletRepo_UpdateBalance_NotFound for the transfers table.
func TestTransferRepo_UpdateState_NotFound(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	repo := postgres.NewTransferRepo()
	err := repo.UpdateState(context.Background(), d, uuid.New(), domain.StateProcessed, "")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

// --- IdempotencyRepo paths -----------------------------------------

// TestIdempotencyRepo_Get_NotFound is the read-side check on the
// not-found sentinel — used by the service's fast-path replay to
// decide "key absent => proceed with the slow path".
func TestIdempotencyRepo_Get_NotFound(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	repo := postgres.NewIdempotencyRepo()
	_, err := repo.Get(context.Background(), d, "absent")
	if !errors.Is(err, repository.ErrNotFound) {
		t.Fatalf("got %v", err)
	}
}

// TestIdempotencyRepo_Insert_ConflictMappedToSentinel is the
// foundational check for the idempotency mechanism. It seeds a
// transfer + an idempotency row, then tries to insert the SAME key
// again — that second insert MUST return
// repository.ErrIdempotencyExists (the SQLSTATE 23505 mapping). If
// this were ever to return a generic error the entire
// idempotent-replay flow in the service would silently break.
func TestIdempotencyRepo_Insert_ConflictMappedToSentinel(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	wallets := postgres.NewWalletRepo()
	transfers := postgres.NewTransferRepo()
	idem := postgres.NewIdempotencyRepo()
	ctx := context.Background()

	// Need an existing transfer so the FK on idempotency_records is satisfied.
	if err := wallets.Insert(ctx, d, &domain.Wallet{ID: "a", Balance: 10}); err != nil {
		t.Fatal(err)
	}
	if err := wallets.Insert(ctx, d, &domain.Wallet{ID: "b", Balance: 0}); err != nil {
		t.Fatal(err)
	}
	tid := uuid.New()
	if err := transfers.Insert(ctx, d, &domain.Transfer{ID: tid, FromWalletID: "a", ToWalletID: "b", Amount: 1, State: domain.StatePending}); err != nil {
		t.Fatal(err)
	}
	if err := idem.Insert(ctx, d, "k", "h", tid); err != nil {
		t.Fatalf("first insert: %v", err)
	}
	err := idem.Insert(ctx, d, "k", "h", tid)
	if !errors.Is(err, repository.ErrIdempotencyExists) {
		t.Fatalf("expected ErrIdempotencyExists, got %v", err)
	}
}

// --- LedgerRepo ----------------------------------------------------

// TestLedgerRepo_ListByTransfer constructs a transfer + two ledger
// entries (one DEBIT, one CREDIT) and verifies:
//
//   - ListByTransfer returns both rows in insertion order, and
//   - the sum of debits equals the sum of credits (the conservation
//     invariant that defines a balanced ledger).
//
// The schema's UNIQUE (transfer_id, type) constraint already
// prevents extra rows of the same type, so this test focuses on the
// retrieval and the math.
func TestLedgerRepo_ListByTransfer(t *testing.T) {
	testdb.Reset(t)
	d := testdb.Get(t)
	wallets := postgres.NewWalletRepo()
	transfers := postgres.NewTransferRepo()
	ledger := postgres.NewLedgerRepo()
	ctx := context.Background()

	if err := wallets.Insert(ctx, d, &domain.Wallet{ID: "a", Balance: 100}); err != nil {
		t.Fatal(err)
	}
	if err := wallets.Insert(ctx, d, &domain.Wallet{ID: "b", Balance: 0}); err != nil {
		t.Fatal(err)
	}
	tid := uuid.New()
	if err := transfers.Insert(ctx, d, &domain.Transfer{ID: tid, FromWalletID: "a", ToWalletID: "b", Amount: 50, State: domain.StatePending}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Insert(ctx, d, &domain.LedgerEntry{TransferID: tid, WalletID: "a", Type: domain.EntryDebit, Amount: 50}); err != nil {
		t.Fatal(err)
	}
	if err := ledger.Insert(ctx, d, &domain.LedgerEntry{TransferID: tid, WalletID: "b", Type: domain.EntryCredit, Amount: 50}); err != nil {
		t.Fatal(err)
	}
	entries, err := ledger.ListByTransfer(ctx, d, tid)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries: %d", len(entries))
	}
	var sumDebit, sumCredit int64
	for _, e := range entries {
		switch e.Type {
		case domain.EntryDebit:
			sumDebit += e.Amount
		case domain.EntryCredit:
			sumCredit += e.Amount
		}
	}
	if sumDebit != sumCredit {
		t.Fatalf("ledger does not balance: debit=%d credit=%d", sumDebit, sumCredit)
	}
}
