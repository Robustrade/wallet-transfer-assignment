//go:build integration

package service

import (
	"context"
	"sync"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/Nandukrishna-S/wallet-transfer-assignment/internal/domain"
)

// TestTransfer_ConcurrencyStress is the "hero" test: N goroutines
// transferring from the same wallet pair concurrently must land on the
// exact final balance and exactly 2N ledger rows — no double-spend, no
// lost update. Run with -race.
func TestTransfer_ConcurrencyStress(t *testing.T) {
	const n = 25
	const amount = "1.00"
	from := newTestWallet(t, "25.00")
	to := newTestWallet(t, "0.00")

	svc := newIntegrationTransferService(t)

	// Minted up front, in the test goroutine: t.Fatalf inside a spawned
	// goroutine only halts that goroutine (via runtime.Goexit), not the
	// test, so any minting error must surface here instead.
	ids := make([]int64, n)
	for i := range ids {
		ids[i] = nextTestTransferID(t)
	}

	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := TransferRequest{
				ID:               ids[i],
				FromWalletID:     from,
				ToWalletID:       to,
				Amount:           decimal.RequireFromString(amount),
				AmountMinorUnits: 100,
			}
			_, err := svc.Transfer(context.Background(), req)
			errs[i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d: unexpected error: %v", i, err)
		}
	}

	ctx := context.Background()
	assertWalletBalance(t, from, "0.00")
	assertWalletBalance(t, to, "25.00")

	var ledgerCount int
	if err := testPool.QueryRow(ctx, `
		SELECT count(*) FROM ledger_entries WHERE wallet_id IN ($1, $2)
	`, from, to).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if ledgerCount != 2*n {
		t.Fatalf("expected exactly %d ledger entries, got %d", 2*n, ledgerCount)
	}
}

// TestTransfer_DoubleClickRace is the second "hero" test: two callers
// submitting the identical idempotency key near-simultaneously must
// produce exactly one transfers row and identical responses — Postgres's
// speculative-insert wait behavior resolving the race, not a
// hypothetical. Run with -race.
func TestTransfer_DoubleClickRace(t *testing.T) {
	from := newTestWallet(t, "10.00")
	to := newTestWallet(t, "0.00")
	svc := newIntegrationTransferService(t)

	req := TransferRequest{
		ID:               nextTestTransferID(t),
		FromWalletID:     from,
		ToWalletID:       to,
		Amount:           decimal.RequireFromString("5.00"),
		AmountMinorUnits: 500,
	}

	var wg sync.WaitGroup
	results := make([]*domain.Transfer, 2)
	errs := make([]error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i], errs[i] = svc.Transfer(context.Background(), req)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("caller %d: unexpected error: %v", i, err)
		}
	}
	if results[0].ID != results[1].ID || results[0].Status != results[1].Status {
		t.Fatalf("expected identical responses, got %+v and %+v", results[0], results[1])
	}

	ctx := context.Background()
	var transferCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM transfers WHERE id = $1`, req.ID).Scan(&transferCount); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if transferCount != 1 {
		t.Fatalf("expected exactly 1 transfers row, got %d", transferCount)
	}

	assertWalletBalance(t, from, "5.00")
	assertWalletBalance(t, to, "5.00")

	var ledgerCount int
	if err := testPool.QueryRow(ctx, `SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, req.ID).Scan(&ledgerCount); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	if ledgerCount != 2 {
		t.Fatalf("expected exactly 2 ledger entries (one execution, not two), got %d", ledgerCount)
	}
}

func assertWalletBalance(t *testing.T, walletID, want string) {
	t.Helper()
	var got decimal.Decimal
	if err := testPool.QueryRow(context.Background(), `SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&got); err != nil {
		t.Fatalf("read wallet balance: %v", err)
	}
	if !got.Equal(decimal.RequireFromString(want)) {
		t.Fatalf("wallet %s: expected balance %s, got %s", walletID, want, got)
	}
}
