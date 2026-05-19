package transfer_test

import (
	"context"
	"errors"
	"testing"

	"github.com/hariharandr/wallet-transfer-assignment/internal/apperr"
	"github.com/hariharandr/wallet-transfer-assignment/internal/domain"
	"github.com/hariharandr/wallet-transfer-assignment/internal/transfer"
	"github.com/hariharandr/wallet-transfer-assignment/testsupport"
)

func TestService_Transfer(t *testing.T) {
	pool := testsupport.StartPostgres(t)
	svc := transfer.NewService(transfer.NewPgxRepository(pool))
	ctx := context.Background()

	t.Run("happy path moves money and writes double entry", func(t *testing.T) {
		testsupport.Reset(t, pool)

		res, err := svc.Transfer(ctx, transfer.Request{
			IdempotencyKey: "k1",
			FromWalletID:   "wallet_1",
			ToWalletID:     "wallet_2",
			Amount:         3000,
		})
		if err != nil {
			t.Fatalf("transfer: %v", err)
		}
		if res.State != domain.StateProcessed {
			t.Fatalf("state = %v, want PROCESSED", res.State)
		}
		if res.TransferID == "" {
			t.Fatal("expected a transfer id back")
		}

		if got := bal(t, pool, "wallet_1"); got != 97000 {
			t.Fatalf("from balance = %d, want 97000", got)
		}
		if got := bal(t, pool, "wallet_2"); got != 53000 {
			t.Fatalf("to balance = %d, want 53000", got)
		}

		var n int
		_ = pool.QueryRow(ctx, `select count(*) from ledger_entries where transfer_id=$1`, res.TransferID).Scan(&n)
		if n != 2 {
			t.Fatalf("ledger rows = %d, want 2", n)
		}
		var dr, cr int64
		_ = pool.QueryRow(ctx, `select coalesce(sum(amount),0) from ledger_entries where type='DEBIT'`).Scan(&dr)
		_ = pool.QueryRow(ctx, `select coalesce(sum(amount),0) from ledger_entries where type='CREDIT'`).Scan(&cr)
		if dr != cr {
			t.Fatalf("ledger unbalanced debit=%d credit=%d", dr, cr)
		}
	})

	t.Run("rejects non positive amount", func(t *testing.T) {
		testsupport.Reset(t, pool)
		_, err := svc.Transfer(ctx, transfer.Request{
			IdempotencyKey: "k2", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 0,
		})
		if !errors.Is(err, domain.ErrInvalidAmount) {
			t.Fatalf("want ErrInvalidAmount, got %v", err)
		}
	})

	t.Run("rejects same source and destination", func(t *testing.T) {
		testsupport.Reset(t, pool)
		_, err := svc.Transfer(ctx, transfer.Request{
			IdempotencyKey: "k3", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 100,
		})
		if !errors.Is(err, apperr.ErrSameWallet) {
			t.Fatalf("want ErrSameWallet, got %v", err)
		}
	})
}
