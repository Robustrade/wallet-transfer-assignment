package transfer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/hariharandr/wallet-transfer-assignment/internal/apperr"
	"github.com/hariharandr/wallet-transfer-assignment/internal/domain"
	"github.com/hariharandr/wallet-transfer-assignment/internal/transfer"
	"github.com/hariharandr/wallet-transfer-assignment/testsupport"
)

func TestPgxRepository(t *testing.T) {
	pool := testsupport.StartPostgres(t)
	repo := transfer.NewPgxRepository(pool)
	ctx := context.Background()

	t.Run("commit persists and rollback discards", func(t *testing.T) {
		testsupport.Reset(t, pool)

		err := repo.WithinTx(ctx, func(tx transfer.Tx) error {
			return tx.AdjustBalance(ctx, "wallet_1", -1000)
		})
		if err != nil {
			t.Fatalf("withinTx commit: %v", err)
		}
		if got := bal(t, pool, "wallet_1"); got != 99000 {
			t.Fatalf("balance = %d, want 99000", got)
		}

		boom := errors.New("boom")
		err = repo.WithinTx(ctx, func(tx transfer.Tx) error {
			if e := tx.AdjustBalance(ctx, "wallet_1", -5000); e != nil {
				return e
			}
			return boom
		})
		if !errors.Is(err, boom) {
			t.Fatalf("want boom, got %v", err)
		}
		if got := bal(t, pool, "wallet_1"); got != 99000 {
			t.Fatalf("rollback failed, balance = %d", got)
		}
	})

	t.Run("lock missing wallet errors", func(t *testing.T) {
		testsupport.Reset(t, pool)
		err := repo.WithinTx(ctx, func(tx transfer.Tx) error {
			_, e := tx.LockWallets(ctx, "wallet_1", "ghost")
			return e
		})
		if !errors.Is(err, apperr.ErrWalletNotFound) {
			t.Fatalf("want ErrWalletNotFound, got %v", err)
		}
	})

	t.Run("happy path moves money with two balanced ledger rows", func(t *testing.T) {
		testsupport.Reset(t, pool)
		var id string
		err := repo.WithinTx(ctx, func(tx transfer.Tx) error {
			if _, e := tx.LockWallets(ctx, "wallet_1", "wallet_2"); e != nil {
				return e
			}
			var e error
			id, e = tx.InsertTransfer(ctx, domain.Transfer{
				FromWallet: "wallet_1", ToWallet: "wallet_2",
				Amount: 2500, State: domain.StatePending,
			})
			if e != nil {
				return e
			}
			if e = tx.InsertLedgerEntry(ctx, domain.LedgerEntry{TransferID: id, WalletID: "wallet_1", Type: domain.Debit, Amount: 2500}); e != nil {
				return e
			}
			if e = tx.InsertLedgerEntry(ctx, domain.LedgerEntry{TransferID: id, WalletID: "wallet_2", Type: domain.Credit, Amount: 2500}); e != nil {
				return e
			}
			if e = tx.AdjustBalance(ctx, "wallet_1", -2500); e != nil {
				return e
			}
			if e = tx.AdjustBalance(ctx, "wallet_2", 2500); e != nil {
				return e
			}
			return tx.UpdateTransferState(ctx, id, domain.StateProcessed, "")
		})
		if err != nil {
			t.Fatalf("withinTx: %v", err)
		}

		if bal(t, pool, "wallet_1") != 97500 {
			t.Fatalf("from balance wrong: %d", bal(t, pool, "wallet_1"))
		}
		if bal(t, pool, "wallet_2") != 52500 {
			t.Fatalf("to balance wrong: %d", bal(t, pool, "wallet_2"))
		}

		var n int
		if e := pool.QueryRow(ctx, `select count(*) from ledger_entries where transfer_id=$1`, id).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n != 2 {
			t.Fatalf("want 2 ledger rows, got %d", n)
		}

		var dr, cr int64
		_ = pool.QueryRow(ctx, `select coalesce(sum(amount),0) from ledger_entries where type='DEBIT'`).Scan(&dr)
		_ = pool.QueryRow(ctx, `select coalesce(sum(amount),0) from ledger_entries where type='CREDIT'`).Scan(&cr)
		if dr != cr {
			t.Fatalf("ledger not balanced: debit=%d credit=%d", dr, cr)
		}

		var status string
		_ = pool.QueryRow(ctx, `select status from transfers where id=$1`, id).Scan(&status)
		if status != "PROCESSED" {
			t.Fatalf("status = %s, want PROCESSED", status)
		}
	})

	t.Run("overdraft blocked by db check", func(t *testing.T) {
		testsupport.Reset(t, pool)
		err := repo.WithinTx(ctx, func(tx transfer.Tx) error {
			return tx.AdjustBalance(ctx, "wallet_3", -1) // seeded at 0
		})
		if err == nil {
			t.Fatal("expected check violation on negative balance")
		}
	})
}

func bal(t *testing.T, pool *pgxpool.Pool, id string) int64 {
	t.Helper()
	var b int64
	if err := pool.QueryRow(context.Background(),
		`select balance from wallets where id=$1`, id).Scan(&b); err != nil {
		t.Fatalf("read balance %s: %v", id, err)
	}
	return b
}
