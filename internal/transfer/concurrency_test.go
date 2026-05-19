package transfer_test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/hariharandr/wallet-transfer-assignment/internal/apperr"
	"github.com/hariharandr/wallet-transfer-assignment/internal/transfer"
	"github.com/hariharandr/wallet-transfer-assignment/testsupport"
)

func TestConcurrency(t *testing.T) {
	pool := testsupport.StartPostgres(t)
	svc := transfer.NewService(transfer.NewPgxRepository(pool))
	ctx := context.Background()

	t.Run("concurrent debits on one wallet stay correct", func(t *testing.T) {
		testsupport.Reset(t, pool)
		const n = 20

		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_, err := svc.Transfer(ctx, transfer.Request{
					IdempotencyKey: fmt.Sprintf("c-%d", i),
					FromWalletID:   "wallet_1",
					ToWalletID:     "wallet_2",
					Amount:         1000,
				})
				errs <- err
			}(i)
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			if err != nil {
				t.Fatalf("transfer failed under load: %v", err)
			}
		}
		if got := bal(t, pool, "wallet_1"); got != 100000-n*1000 {
			t.Fatalf("wallet_1 = %d, want %d", got, 100000-n*1000)
		}
		if got := bal(t, pool, "wallet_2"); got != 50000+n*1000 {
			t.Fatalf("wallet_2 = %d, want %d", got, 50000+n*1000)
		}
		var cnt int
		_ = pool.QueryRow(ctx, `select count(*) from transfers where status='PROCESSED'`).Scan(&cnt)
		if cnt != n {
			t.Fatalf("processed transfers = %d, want %d", cnt, n)
		}
		var dr, cr int64
		_ = pool.QueryRow(ctx, `select coalesce(sum(amount),0) from ledger_entries where type='DEBIT'`).Scan(&dr)
		_ = pool.QueryRow(ctx, `select coalesce(sum(amount),0) from ledger_entries where type='CREDIT'`).Scan(&cr)
		if dr != cr {
			t.Fatalf("ledger unbalanced: debit=%d credit=%d", dr, cr)
		}
	})

	t.Run("same key fired concurrently moves money once", func(t *testing.T) {
		testsupport.Reset(t, pool)
		const n = 15

		var wg sync.WaitGroup
		errs := make(chan error, n)
		for i := 0; i < n; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, err := svc.Transfer(ctx, transfer.Request{
					IdempotencyKey: "race1",
					FromWalletID:   "wallet_1",
					ToWalletID:     "wallet_2",
					Amount:         5000,
				})
				errs <- err
			}()
		}
		wg.Wait()
		close(errs)

		for err := range errs {
			if err != nil && !errors.Is(err, apperr.ErrInProgress) {
				t.Fatalf("unexpected error: %v", err)
			}
		}
		var cnt int
		_ = pool.QueryRow(ctx, `select count(*) from transfers`).Scan(&cnt)
		if cnt != 1 {
			t.Fatalf("transfers = %d, want exactly 1", cnt)
		}
		if got := bal(t, pool, "wallet_1"); got != 95000 {
			t.Fatalf("wallet_1 = %d, want 95000 (moved once)", got)
		}
		if got := bal(t, pool, "wallet_2"); got != 55000 {
			t.Fatalf("wallet_2 = %d, want 55000 (moved once)", got)
		}
	})
}
