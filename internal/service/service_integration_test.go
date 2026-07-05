//go:build integration

package service_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/repository"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/service"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/testutil"
)

func newTestService(t *testing.T) (*service.TransferService, *pgxpool.Pool) {
	t.Helper()
	pool := testutil.NewPool(t)
	testutil.TruncateAll(t, pool)
	return service.NewTransferService(repository.NewStore(pool)), pool
}

func mustScan(t *testing.T, pool *pgxpool.Pool, query string, args []any, dest ...any) {
	t.Helper()
	if err := pool.QueryRow(context.Background(), query, args...).Scan(dest...); err != nil {
		t.Fatalf("query %q: %v", query, err)
	}
}

func TestCreateTransfer_SufficientBalance_Processes(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 500)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	transfer, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-sufficient",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer.Status != domain.StatusProcessed {
		t.Fatalf("expected status PROCESSED, got %s", transfer.Status)
	}

	var fromBalance, toBalance int64
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_1"}, &fromBalance)
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_2"}, &toBalance)
	if fromBalance != 300 {
		t.Errorf("expected from balance 300, got %d", fromBalance)
	}
	if toBalance != 200 {
		t.Errorf("expected to balance 200, got %d", toBalance)
	}

	var ledgerCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, []any{transfer.ID}, &ledgerCount)
	if ledgerCount != 2 {
		t.Errorf("expected 2 ledger entries, got %d", ledgerCount)
	}
}

func TestCreateTransfer_InsufficientBalance_Fails(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 50)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	transfer, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-insufficient",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if transfer.Status != domain.StatusFailed {
		t.Fatalf("expected status FAILED, got %s", transfer.Status)
	}
	if transfer.FailureReason == nil || *transfer.FailureReason != "insufficient balance" {
		t.Errorf("expected failure reason %q, got %v", "insufficient balance", transfer.FailureReason)
	}

	var fromBalance, toBalance int64
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_1"}, &fromBalance)
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_2"}, &toBalance)
	if fromBalance != 50 {
		t.Errorf("expected from balance unchanged at 50, got %d", fromBalance)
	}
	if toBalance != 0 {
		t.Errorf("expected to balance unchanged at 0, got %d", toBalance)
	}

	var ledgerCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, []any{transfer.ID}, &ledgerCount)
	if ledgerCount != 0 {
		t.Errorf("expected no ledger entries for a failed transfer, got %d", ledgerCount)
	}
}

func TestCreateTransfer_SameWallet_Rejected(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 100)

	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-same-wallet",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         50,
	})
	if !errors.Is(err, domain.ErrSameWallet) {
		t.Errorf("expected ErrSameWallet, got %v", err)
	}

	var transferCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM transfers`, nil, &transferCount)
	if transferCount != 0 {
		t.Errorf("expected no transfer to be persisted, got %d", transferCount)
	}
}

func TestCreateTransfer_UnknownWallet_Rejected(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 100)

	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: "key-unknown-wallet",
		FromWalletID:   "wallet_1",
		ToWalletID:     "does-not-exist",
		Amount:         50,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Errorf("expected ErrWalletNotFound, got %v", err)
	}

	var transferCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM transfers`, nil, &transferCount)
	if transferCount != 0 {
		t.Errorf("expected no transfer to be persisted, got %d", transferCount)
	}
}

func TestCreateTransfer_NonPositiveAmount_Rejected(t *testing.T) {
	cases := []struct {
		name   string
		amount int64
	}{
		{name: "zero amount", amount: 0},
		{name: "negative amount", amount: -50},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, pool := newTestService(t)
			testutil.SeedWallet(t, pool, "wallet_1", 100)
			testutil.SeedWallet(t, pool, "wallet_2", 0)

			_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
				IdempotencyKey: "key-" + tc.name,
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         tc.amount,
			})
			if !errors.Is(err, domain.ErrInvalidAmount) {
				t.Errorf("expected ErrInvalidAmount, got %v", err)
			}
		})
	}
}

func TestCreateTransfer_IdempotentReplay_SameBody(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 500)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	req := service.CreateTransferRequest{
		IdempotencyKey: "key-replay",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	second, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("expected replay to return the same transfer ID %s, got %s", first.ID, second.ID)
	}
	if second.Status != domain.StatusProcessed {
		t.Errorf("expected replay status PROCESSED, got %s", second.Status)
	}

	var fromBalance int64
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_1"}, &fromBalance)
	if fromBalance != 400 {
		t.Errorf("expected wallet_1 debited exactly once to 400, got %d", fromBalance)
	}

	var ledgerCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, []any{first.ID}, &ledgerCount)
	if ledgerCount != 2 {
		t.Errorf("expected exactly 2 ledger entries total across both calls, got %d", ledgerCount)
	}
}

func TestCreateTransfer_IdempotencyConflict_DifferentBody(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 500)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	key := "key-conflict"
	if _, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}); err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}

	_, err := svc.CreateTransfer(context.Background(), service.CreateTransferRequest{
		IdempotencyKey: key,
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         999,
	})
	if !errors.Is(err, domain.ErrIdempotencyKeyConflict) {
		t.Errorf("expected ErrIdempotencyKeyConflict, got %v", err)
	}

	var transferCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM transfers`, nil, &transferCount)
	if transferCount != 1 {
		t.Errorf("expected exactly 1 transfer to exist, got %d", transferCount)
	}
}

// TestCreateTransfer_IdempotentReplay_FailedTransfer mirrors
// TestCreateTransfer_IdempotentReplay_SameBody, but for a transfer that
// resolved to FAILED (insufficient balance) rather than PROCESSED. Replaying
// the same key must return the same FAILED transfer again, not attempt to
// reprocess it or return an error.
func TestCreateTransfer_IdempotentReplay_FailedTransfer(t *testing.T) {
	svc, pool := newTestService(t)
	testutil.SeedWallet(t, pool, "wallet_1", 50)
	testutil.SeedWallet(t, pool, "wallet_2", 0)

	req := service.CreateTransferRequest{
		IdempotencyKey: "key-replay-failed",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if first.Status != domain.StatusFailed {
		t.Fatalf("expected first call status FAILED, got %s", first.Status)
	}

	second, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("second call: unexpected error: %v", err)
	}
	if second.ID != first.ID {
		t.Errorf("expected replay to return the same transfer ID %s, got %s", first.ID, second.ID)
	}
	if second.Status != domain.StatusFailed {
		t.Errorf("expected replay status FAILED, got %s", second.Status)
	}
	if second.FailureReason == nil || *second.FailureReason != "insufficient balance" {
		t.Errorf("expected replay failure reason %q, got %v", "insufficient balance", second.FailureReason)
	}

	var fromBalance, toBalance int64
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_1"}, &fromBalance)
	mustScan(t, pool, `SELECT balance FROM wallets WHERE id = $1`, []any{"wallet_2"}, &toBalance)
	if fromBalance != 50 {
		t.Errorf("expected wallet_1 balance unchanged at 50, got %d", fromBalance)
	}
	if toBalance != 0 {
		t.Errorf("expected wallet_2 balance unchanged at 0, got %d", toBalance)
	}

	var transferCount int
	mustScan(t, pool, `SELECT COUNT(*) FROM transfers`, nil, &transferCount)
	if transferCount != 1 {
		t.Errorf("expected exactly 1 transfer to exist, got %d", transferCount)
	}
}
