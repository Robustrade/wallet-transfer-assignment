package repository_test

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/repository"
)

// Integration tests run only when DATABASE_URL is set (e.g. `make itest`).
// They use uniquely generated ids and clean up their own rows, so they are
// safe against a shared remote (Neon) database.

var testPool *pgxpool.Pool

func TestMain(m *testing.M) {
	dsn := strings.Trim(os.Getenv("DATABASE_URL"), `"'`)
	if dsn != "" {
		pool, err := repository.NewPool(context.Background(), dsn)
		if err != nil {
			panic("connect test pool: " + err.Error())
		}
		testPool = pool
	}
	code := m.Run()
	if testPool != nil {
		testPool.Close()
	}
	os.Exit(code)
}

func requireDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	if testPool == nil {
		t.Skip("DATABASE_URL not set; skipping integration test")
	}
	return testPool
}

var idCounter atomic.Int64

func uniqueID(prefix string) string {
	return prefix + "_" + time.Now().Format("150405.000000") + "_" +
		decimal.NewFromInt(idCounter.Add(1)).String()
}

func seedWallet(t *testing.T, pool *pgxpool.Pool, balance string) string {
	t.Helper()
	id := uniqueID("w")
	_, err := pool.Exec(context.Background(),
		`INSERT INTO wallets (id, name, balance) VALUES ($1, $2, $3)`,
		id, "test wallet", decimal.RequireFromString(balance))
	if err != nil {
		t.Fatalf("seed wallet: %v", err)
	}
	t.Cleanup(func() { cleanupWallets(pool, id) })
	return id
}

func cleanupWallets(pool *pgxpool.Pool, ids ...string) {
	ctx := context.Background()
	_, _ = pool.Exec(ctx, `DELETE FROM ledger_entries WHERE wallet_id = ANY($1)`, ids)
	_, _ = pool.Exec(ctx,
		`DELETE FROM transfers WHERE from_wallet_id = ANY($1) OR to_wallet_id = ANY($1)`, ids)
	_, _ = pool.Exec(ctx, `DELETE FROM wallets WHERE id = ANY($1)`, ids)
}

func TestWalletRepository_GetForUpdate_NotFound(t *testing.T) {
	pool := requireDB(t)
	repo := repository.NewWalletRepository()
	_, err := repo.GetForUpdate(context.Background(), pool, uniqueID("missing"))
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("got %v, want ErrWalletNotFound", err)
	}
}

func TestWalletRepository_LockAndAddToBalance(t *testing.T) {
	pool := requireDB(t)
	repo := repository.NewWalletRepository()
	id := seedWallet(t, pool, "100.00")
	ctx := context.Background()

	err := repository.WithTx(ctx, pool, func(tx pgx.Tx) error {
		w, err := repo.GetForUpdate(ctx, tx, id)
		if err != nil {
			return err
		}
		if !w.Balance.Equal(decimal.RequireFromString("100.00")) {
			t.Fatalf("locked balance = %s, want 100.00", w.Balance)
		}
		return repo.AddToBalance(ctx, tx, id, decimal.RequireFromString("-30.50"))
	})
	if err != nil {
		t.Fatalf("tx: %v", err)
	}

	w, err := repo.GetByID(ctx, pool, id)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if !w.Balance.Equal(decimal.RequireFromString("69.50")) {
		t.Fatalf("balance = %s, want 69.50", w.Balance)
	}
}

func TestTransferRepository_InsertPending_DuplicateKey(t *testing.T) {
	pool := requireDB(t)
	tr := repository.NewTransferRepository()
	ctx := context.Background()
	from := seedWallet(t, pool, "100.00")
	to := seedWallet(t, pool, "0.00")
	key := uniqueID("k")

	first, err := tr.InsertPending(ctx, pool, domain.Transfer{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("10.00"),
	})
	if err != nil {
		t.Fatalf("first insert: %v", err)
	}
	if first.Status != domain.TransferPending {
		t.Fatalf("status = %s, want PENDING", first.Status)
	}

	_, err = tr.InsertPending(ctx, pool, domain.Transfer{
		IdempotencyKey: key, FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("10.00"),
	})
	if !errors.Is(err, domain.ErrDuplicateIdempotencyKey) {
		t.Fatalf("got %v, want ErrDuplicateIdempotencyKey", err)
	}

	got, err := tr.GetByIdempotencyKey(ctx, pool, key)
	if err != nil || got.ID != first.ID {
		t.Fatalf("get by key: id=%s err=%v, want id=%s", got.ID, err, first.ID)
	}
}

func TestTransferRepository_UpdateStatus(t *testing.T) {
	pool := requireDB(t)
	tr := repository.NewTransferRepository()
	ctx := context.Background()
	from := seedWallet(t, pool, "100.00")
	to := seedWallet(t, pool, "0.00")

	tx, err := tr.InsertPending(ctx, pool, domain.Transfer{
		IdempotencyKey: uniqueID("k"), FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("5.00"),
	})
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := tr.UpdateStatus(ctx, pool, tx.ID, domain.TransferFailed, "insufficient funds"); err != nil {
		t.Fatalf("update status: %v", err)
	}
	got, err := tr.GetByID(ctx, pool, tx.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Status != domain.TransferFailed || got.FailureReason != "insufficient funds" {
		t.Fatalf("got status=%s reason=%q, want FAILED / insufficient funds",
			got.Status, got.FailureReason)
	}
	if !got.UpdatedAt.After(tx.UpdatedAt) {
		t.Fatalf("updated_at not advanced: %v !> %v", got.UpdatedAt, tx.UpdatedAt)
	}
}

func TestLedgerRepository_DoubleEntryAndDuplicate(t *testing.T) {
	pool := requireDB(t)
	tr := repository.NewTransferRepository()
	lr := repository.NewLedgerRepository()
	ctx := context.Background()
	from := seedWallet(t, pool, "100.00")
	to := seedWallet(t, pool, "0.00")

	transfer, err := tr.InsertPending(ctx, pool, domain.Transfer{
		IdempotencyKey: uniqueID("k"), FromWalletID: from, ToWalletID: to,
		Amount: decimal.RequireFromString("10.00"),
	})
	if err != nil {
		t.Fatalf("insert transfer: %v", err)
	}
	amt := decimal.RequireFromString("10.00")

	if err := lr.InsertEntry(ctx, pool, domain.LedgerEntry{
		TransferID: transfer.ID, WalletID: from, Type: domain.LedgerDebit, Amount: amt,
	}); err != nil {
		t.Fatalf("insert debit: %v", err)
	}
	if err := lr.InsertEntry(ctx, pool, domain.LedgerEntry{
		TransferID: transfer.ID, WalletID: to, Type: domain.LedgerCredit, Amount: amt,
	}); err != nil {
		t.Fatalf("insert credit: %v", err)
	}

	// Second DEBIT for the same transfer must violate UNIQUE(transfer_id,type).
	err = lr.InsertEntry(ctx, pool, domain.LedgerEntry{
		TransferID: transfer.ID, WalletID: from, Type: domain.LedgerDebit, Amount: amt,
	})
	if err == nil {
		t.Fatal("expected unique violation on duplicate DEBIT, got nil")
	}
}
