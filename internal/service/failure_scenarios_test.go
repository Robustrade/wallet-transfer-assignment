package service_test

import (
	"context"
	"testing"

	"Wallet_Transfer_Service/internal/domain"
	"Wallet_Transfer_Service/internal/service"
)

func TestCreateTransfer_ZeroAmount(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 100)
	mustCreateWallet(t, ws, "wallet_2", 0)

	_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "zero-amount",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         0,
	})
	if err != domain.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestCreateTransfer_NegativeAmount(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 100)
	mustCreateWallet(t, ws, "wallet_2", 0)

	_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "negative-amount",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         -50,
	})
	if err != domain.ErrInvalidAmount {
		t.Fatalf("expected ErrInvalidAmount, got %v", err)
	}
}

func TestCreateTransfer_SameWallet(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 100)

	_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "same-wallet",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         10,
	})
	if err != domain.ErrSameWallet {
		t.Fatalf("expected ErrSameWallet, got %v", err)
	}
}

func TestCreateTransfer_EmptyIdempotencyKey(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 100)
	mustCreateWallet(t, ws, "wallet_2", 0)

	_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         10,
	})
	if err != domain.ErrInvalidRequest {
		t.Fatalf("expected ErrInvalidRequest, got %v", err)
	}
}

func TestCreateTransfer_ToWalletNotFound(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 100)

	_, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "from-missing",
		FromWalletID:   "does_not_exist",
		ToWalletID:     "wallet_1",
		Amount:         10,
	})
	if err != domain.ErrWalletNotFound {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
}

// Transferring exactly the full balance must succeed and drain the
// source wallet to zero - the boundary right at CanDebit's >= check.
func TestCreateTransfer_ExactBalanceSucceeds(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 100)
	mustCreateWallet(t, ws, "wallet_2", 0)

	res, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "exact-balance",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if res.State != domain.TransferProcessed {
		t.Fatalf("expected PROCESSED, got %s", res.State)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	if from.Balance != 0 {
		t.Fatalf("expected source wallet drained to 0, got %d", from.Balance)
	}
}

// One unit over the available balance must fail cleanly, right on the
// other side of the same boundary.
func TestCreateTransfer_OneOverBalanceFails(t *testing.T) {
	ts, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "wallet_1", 100)
	mustCreateWallet(t, ws, "wallet_2", 0)

	res, _, err := ts.CreateTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "one-over",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         101,
	})
	if err != nil {
		t.Fatalf("expected a FAILED result, not an error: %v", err)
	}
	if res.State != domain.TransferFailed {
		t.Fatalf("expected FAILED, got %s", res.State)
	}

	from, _ := ws.GetWallet(ctx, "wallet_1")
	if from.Balance != 100 {
		t.Fatalf("balance must be unchanged, got %d", from.Balance)
	}
}

func TestGetTransfer_NotFound(t *testing.T) {
	ts, _ := newFixture(t)
	ctx := context.Background()

	_, err := ts.GetTransfer(ctx, "txn_does_not_exist")
	if err != domain.ErrTransferNotFound {
		t.Fatalf("expected ErrTransferNotFound, got %v", err)
	}
}

func TestCreateWallet_DuplicateID(t *testing.T) {
	_, ws := newFixture(t)
	ctx := context.Background()
	mustCreateWallet(t, ws, "dup", 10)

	_, err := ws.CreateWallet(ctx, "dup", 20)
	if err != domain.ErrWalletExists {
		t.Fatalf("expected ErrWalletExists, got %v", err)
	}
}

func TestCreateWallet_NegativeInitialBalance(t *testing.T) {
	_, ws := newFixture(t)
	ctx := context.Background()

	_, err := ws.CreateWallet(ctx, "neg", -1)
	if err != domain.ErrNegativeBalance {
		t.Fatalf("expected ErrNegativeBalance, got %v", err)
	}
}
