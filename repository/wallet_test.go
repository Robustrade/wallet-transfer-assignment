package repository_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/helpers"
	"github.com/Robustrade/wallet-transfer-assignment/model"
	"github.com/Robustrade/wallet-transfer-assignment/repository"
	"github.com/jackc/pgx/v5"
)

func TestWalletRepository_CreateGetConflict(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	repo := repository.NewWalletRepository(pool)
	ctx := context.Background()

	w, err := model.NewWallet("wallet_1", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, w); err != nil {
		t.Fatal(err)
	}
	got, err := repo.GetByID(ctx, "wallet_1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Balance != 1000 || got.OpeningBalance != 1000 {
		t.Fatalf("%+v", got)
	}
	if err := repo.Create(ctx, w); err != model.ErrConflict {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := repo.GetByID(ctx, "missing"); err != model.ErrNotFound {
		t.Fatalf("missing: %v", err)
	}
}

func TestWalletRepository_DebitCreditAndLock(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	repo := repository.NewWalletRepository(pool)
	transfers := repository.NewTransferRepository(pool)
	ledger := repository.NewLedgerRepository(pool)
	idem := repository.NewIdempotencyRepository(pool)
	ctx := context.Background()

	a, _ := model.NewWallet("a", 100)
	b, _ := model.NewWallet("b", 0)
	_ = repo.Create(ctx, a)
	_ = repo.Create(ctx, b)

	hash, body, err := model.HashTransferRequest("a", "b", 40)
	if err != nil {
		t.Fatal(err)
	}
	rec, err := model.NewIdempotencyRecord("k-lock", hash, body)
	if err != nil {
		t.Fatal(err)
	}
	if claimed, err := idem.Claim(ctx, rec); err != nil || !claimed {
		t.Fatalf("claim: %v %v", claimed, err)
	}

	txCtx, tx, err := repository.BeginTx(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	locked, err := repo.LockByIDs(txCtx, "b", "a") // order of args must not matter
	if err != nil {
		t.Fatal(err)
	}
	if len(locked) != 2 || locked[0].ID != "a" || locked[1].ID != "b" {
		t.Fatalf("lock order: %+v", locked)
	}

	tr, err := model.NewTransfer("k-lock", "a", "b", 40)
	if err != nil {
		t.Fatal(err)
	}
	if err := transfers.Insert(txCtx, tr); err != nil {
		t.Fatal(err)
	}

	amt, _ := model.NewAmount(40)
	ok, err := repo.Debit(txCtx, "a", amt)
	if err != nil || !ok {
		t.Fatalf("debit: ok=%v err=%v", ok, err)
	}
	if err := repo.Credit(txCtx, "b", amt); err != nil {
		t.Fatal(err)
	}
	ok, err = repo.Debit(txCtx, "a", helpers.Money(100))
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("expected insufficient funds")
	}

	if err := tr.MarkProcessed(); err != nil {
		t.Fatal(err)
	}
	debit, credit, err := model.NewLedgerPair(tr)
	if err != nil {
		t.Fatal(err)
	}
	if err := ledger.InsertPair(txCtx, debit, credit); err != nil {
		t.Fatal(err)
	}
	if err := transfers.UpdateStatus(txCtx, tr); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	wa, _ := repo.GetByID(ctx, "a")
	wb, _ := repo.GetByID(ctx, "b")
	if wa.Balance != 60 || wb.Balance != 40 {
		t.Fatalf("a=%d b=%d", wa.Balance, wb.Balance)
	}
	if wa.OpeningBalance != 100 {
		t.Fatal("opening_balance must not change")
	}
}

func TestWalletRepository_OpeningBalanceImmutable(t *testing.T) {
	pool := requirePool(t)
	truncateAll(t)
	repo := repository.NewWalletRepository(pool)
	ctx := context.Background()

	w, _ := model.NewWallet("immutable_w", 100)
	if err := repo.Create(ctx, w); err != nil {
		t.Fatal(err)
	}

	_, err := pool.Exec(ctx, `UPDATE wallets SET opening_balance = 1 WHERE id = @id`, pgx.NamedArgs{"id": "immutable_w"})
	if err == nil {
		t.Fatal("expected opening_balance update to fail")
	}
	if !strings.Contains(err.Error(), "opening_balance is immutable") {
		t.Fatalf("got %v, want opening_balance immutable", err)
	}

	got, err := repo.GetByID(ctx, "immutable_w")
	if err != nil {
		t.Fatal(err)
	}
	if got.OpeningBalance != 100 {
		t.Fatalf("opening_balance changed to %d", got.OpeningBalance)
	}
}
