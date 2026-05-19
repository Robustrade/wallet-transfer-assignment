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

func TestService_Failures(t *testing.T) {
	pool := testsupport.StartPostgres(t)
	svc := transfer.NewService(transfer.NewPgxRepository(pool))
	ctx := context.Background()

	t.Run("insufficient funds is a replayable FAILED transfer", func(t *testing.T) {
		testsupport.Reset(t, pool)
		req := transfer.Request{IdempotencyKey: "f1", FromWalletID: "wallet_3", ToWalletID: "wallet_1", Amount: 100}

		res, err := svc.Transfer(ctx, req)
		if !errors.Is(err, apperr.ErrInsufficientFunds) {
			t.Fatalf("want ErrInsufficientFunds, got %v", err)
		}
		if res.State != domain.StateFailed || res.TransferID == "" {
			t.Fatalf("want FAILED transfer with id, got %+v", res)
		}
		if bal(t, pool, "wallet_3") != 0 || bal(t, pool, "wallet_1") != 100000 {
			t.Fatalf("balances must not move on failure")
		}
		var ledger int
		_ = pool.QueryRow(ctx, `select count(*) from ledger_entries`).Scan(&ledger)
		if ledger != 0 {
			t.Fatalf("no ledger rows expected, got %d", ledger)
		}
		var status string
		_ = pool.QueryRow(ctx, `select status from transfers where id=$1`, res.TransferID).Scan(&status)
		if status != "FAILED" {
			t.Fatalf("transfer status = %s", status)
		}

		replay, err := svc.Transfer(ctx, req)
		if !errors.Is(err, apperr.ErrInsufficientFunds) || replay.TransferID != res.TransferID {
			t.Fatalf("replay must repeat the same failure, got %+v %v", replay, err)
		}
	})

	t.Run("unknown wallet is rejected and replayable", func(t *testing.T) {
		testsupport.Reset(t, pool)
		req := transfer.Request{IdempotencyKey: "f2", FromWalletID: "ghost", ToWalletID: "wallet_1", Amount: 100}

		if _, err := svc.Transfer(ctx, req); !errors.Is(err, apperr.ErrWalletNotFound) {
			t.Fatalf("want ErrWalletNotFound, got %v", err)
		}
		var n int
		_ = pool.QueryRow(ctx, `select count(*) from transfers`).Scan(&n)
		if n != 0 {
			t.Fatalf("no transfer row expected, got %d", n)
		}
		if _, err := svc.Transfer(ctx, req); !errors.Is(err, apperr.ErrWalletNotFound) {
			t.Fatalf("replay must repeat ErrWalletNotFound, got %v", err)
		}
	})
}
