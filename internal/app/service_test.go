package app

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/robustrade/wallet-transfer-assignment/internal/storage"
)

func newTestService(t *testing.T) *Service {
	t.Helper()
	db, err := storage.NewSQLiteDB("file:memdb?mode=memory&cache=shared&_foreign_keys=1&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := storage.Migrate(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := NewRepository(db)
	return NewService(repo)
}

func TestCreateTransferSuccess(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	walletA, err := svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_1", Balance: 200})
	if err != nil {
		t.Fatalf("create wallet A: %v", err)
	}
	_, err = svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_2", Balance: 50})
	if err != nil {
		t.Fatalf("create wallet B: %v", err)
	}

	result, err := svc.CreateTransfer(ctx, &CreateTransferRequest{IdempotencyKey: "tx1", FromWalletID: walletA.ID, ToWalletID: "wallet_2", Amount: 100})
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if result.Existing {
		t.Fatalf("expected new transfer, got existing")
	}
	if result.Transfer.State != StateProcessed {
		t.Fatalf("expected transfer processed, got %s", result.Transfer.State)
	}

	balanceA, err := svc.GetWallet(ctx, walletA.ID)
	if err != nil {
		t.Fatalf("get wallet A: %v", err)
	}
	if balanceA.Balance != 100 {
		t.Fatalf("expected wallet A balance 100, got %d", balanceA.Balance)
	}

	balanceB, err := svc.GetWallet(ctx, "wallet_2")
	if err != nil {
		t.Fatalf("get wallet B: %v", err)
	}
	if balanceB.Balance != 150 {
		t.Fatalf("expected wallet B balance 150, got %d", balanceB.Balance)
	}
}

func TestTransferIdempotency(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_1", Balance: 300})
	if err != nil {
		t.Fatalf("create wallet A: %v", err)
	}
	_, err = svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_2", Balance: 0})
	if err != nil {
		t.Fatalf("create wallet B: %v", err)
	}

	request := &CreateTransferRequest{IdempotencyKey: "idem-abc", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	firstResult, err := svc.CreateTransfer(ctx, request)
	if err != nil {
		t.Fatalf("first transfer: %v", err)
	}
	if firstResult.Existing {
		t.Fatalf("expected first request to create transfer")
	}

	secondResult, err := svc.CreateTransfer(ctx, request)
	if err != nil {
		t.Fatalf("second transfer: %v", err)
	}
	if !secondResult.Existing {
		t.Fatalf("expected second request to return existing transfer")
	}
	if firstResult.Transfer.ID != secondResult.Transfer.ID {
		t.Fatalf("expected same transfer id for duplicate request")
	}

	balance, err := svc.GetWallet(ctx, "wallet_1")
	if err != nil {
		t.Fatalf("get wallet A: %v", err)
	}
	if balance.Balance != 200 {
		t.Fatalf("expected wallet A balance 200, got %d", balance.Balance)
	}
}

func TestTransferFailsWhenInsufficientFunds(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_1", Balance: 50})
	if err != nil {
		t.Fatalf("create wallet A: %v", err)
	}
	_, err = svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_2", Balance: 0})
	if err != nil {
		t.Fatalf("create wallet B: %v", err)
	}

	request := &CreateTransferRequest{IdempotencyKey: "tx-insufficient", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100}
	result, err := svc.CreateTransfer(ctx, request)
	if err == nil {
		t.Fatal("expected insufficient funds error")
	}
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("expected insufficient funds error, got %v", err)
	}
	if result == nil || result.Transfer.State != StateFailed {
		t.Fatalf("expected failed transfer saved, got %+v", result)
	}

	second, err := svc.CreateTransfer(ctx, request)
	if err == nil {
		t.Fatal("expected insufficient funds error on duplicate request")
	}
	if !second.Existing {
		t.Fatalf("expected duplicate request to return existing transfer")
	}
	if second.Transfer.ID != result.Transfer.ID {
		t.Fatalf("expected same transfer id on duplicate")
	}
}

func TestConcurrentTransfersPreventDoubleSpend(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_1", Balance: 100})
	if err != nil {
		t.Fatalf("create wallet A: %v", err)
	}
	_, err = svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_2", Balance: 0})
	if err != nil {
		t.Fatalf("create wallet B: %v", err)
	}
	_, err = svc.CreateWallet(ctx, &CreateWalletRequest{ID: "wallet_3", Balance: 0})
	if err != nil {
		t.Fatalf("create wallet C: %v", err)
	}

	requests := []*CreateTransferRequest{
		{IdempotencyKey: "concurrent-1", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100},
		{IdempotencyKey: "concurrent-2", FromWalletID: "wallet_1", ToWalletID: "wallet_3", Amount: 100},
	}

	var wg sync.WaitGroup
	results := make([]*TransferResult, len(requests))
	errorsCaptured := make([]error, len(requests))

	for i, req := range requests {
		wg.Add(1)
		go func(index int, request *CreateTransferRequest) {
			defer wg.Done()
			results[index], errorsCaptured[index] = svc.CreateTransfer(ctx, request)
		}(i, req)
	}
	wg.Wait()

	successCount := 0
	for i, err := range errorsCaptured {
		if err == nil {
			successCount++
			continue
		}
		if !errors.Is(err, ErrInsufficientFunds) {
			t.Fatalf("unexpected error for request %d: %v", i, err)
		}
	}
	if successCount != 1 {
		t.Fatalf("expected one successful transfer, got %d", successCount)
	}

	walletA, err := svc.GetWallet(ctx, "wallet_1")
	if err != nil {
		t.Fatalf("get wallet A: %v", err)
	}
	if walletA.Balance != 0 {
		t.Fatalf("expected source wallet balance 0, got %d", walletA.Balance)
	}
}
