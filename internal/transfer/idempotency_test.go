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

func TestService_Idempotency(t *testing.T) {
	pool := testsupport.StartPostgres(t)
	repo := transfer.NewPgxRepository(pool)
	svc := transfer.NewService(repo)
	ctx := context.Background()

	t.Run("same key and payload replays and moves money once", func(t *testing.T) {
		testsupport.Reset(t, pool)
		req := transfer.Request{IdempotencyKey: "dup1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1000}

		first, err := svc.Transfer(ctx, req)
		if err != nil {
			t.Fatalf("first: %v", err)
		}
		second, err := svc.Transfer(ctx, req)
		if err != nil {
			t.Fatalf("second: %v", err)
		}

		if second.TransferID != first.TransferID {
			t.Fatalf("replay id %s != original %s", second.TransferID, first.TransferID)
		}
		if second.State != domain.StateProcessed {
			t.Fatalf("replay state = %v", second.State)
		}
		if bal(t, pool, "wallet_1") != 99000 {
			t.Fatalf("from moved twice: %d", bal(t, pool, "wallet_1"))
		}
		if bal(t, pool, "wallet_2") != 51000 {
			t.Fatalf("to moved twice: %d", bal(t, pool, "wallet_2"))
		}
		var n int
		_ = pool.QueryRow(ctx, `select count(*) from transfers`).Scan(&n)
		if n != 1 {
			t.Fatalf("want 1 transfer row, got %d", n)
		}
	})

	t.Run("same key different payload rejected", func(t *testing.T) {
		testsupport.Reset(t, pool)
		base := transfer.Request{IdempotencyKey: "dup2", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1000}
		if _, err := svc.Transfer(ctx, base); err != nil {
			t.Fatalf("base: %v", err)
		}
		changed := base
		changed.Amount = 2000
		if _, err := svc.Transfer(ctx, changed); !errors.Is(err, apperr.ErrKeyReused) {
			t.Fatalf("want ErrKeyReused, got %v", err)
		}
	})

	t.Run("key still in progress returns in-progress", func(t *testing.T) {
		testsupport.Reset(t, pool)
		fp := transfer.Fingerprint("wallet_1", "wallet_2", 1000)
		inserted, err := repo.InsertIdempotencyPending(ctx, "pend1", fp)
		if err != nil || !inserted {
			t.Fatalf("seed pending: inserted=%v err=%v", inserted, err)
		}
		_, err = svc.Transfer(ctx, transfer.Request{IdempotencyKey: "pend1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1000})
		if !errors.Is(err, apperr.ErrInProgress) {
			t.Fatalf("want ErrInProgress, got %v", err)
		}
	})

	t.Run("missing idempotency key rejected", func(t *testing.T) {
		testsupport.Reset(t, pool)
		_, err := svc.Transfer(ctx, transfer.Request{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 1000})
		if !errors.Is(err, apperr.ErrInvalidRequest) {
			t.Fatalf("want ErrInvalidRequest, got %v", err)
		}
	})
}
