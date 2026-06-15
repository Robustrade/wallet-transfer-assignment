package service_test

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"
	"wallet-transfer-assignment/internal/service"
)

func TestCreateTransferProcessesLedgerAndBalances(t *testing.T) {
	ctx := context.Background()
	transferService := newTestService(t)
	mustCreateWallet(t, transferService, "wallet_1", 1_000)
	mustCreateWallet(t, transferService, "wallet_2", 250)

	result, err := transferService.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "key-success",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}

	if result.Transfer.State != domain.TransferProcessed {
		t.Fatalf("transfer state = %s, want %s", result.Transfer.State, domain.TransferProcessed)
	}
	if len(result.LedgerEntries) != 2 {
		t.Fatalf("ledger entry count = %d, want 2", len(result.LedgerEntries))
	}
	assertLedgerBalances(t, result.LedgerEntries, 100)

	assertWalletBalance(t, transferService, "wallet_1", 900)
	assertWalletBalance(t, transferService, "wallet_2", 350)
}

func TestCreateTransferReplaysSameIdempotencyResult(t *testing.T) {
	ctx := context.Background()
	transferService := newTestService(t)
	mustCreateWallet(t, transferService, "wallet_1", 1_000)
	mustCreateWallet(t, transferService, "wallet_2", 0)

	input := domain.CreateTransferInput{
		IdempotencyKey: "key-replay",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         125,
	}
	first, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}

	second, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("second CreateTransfer returned error: %v", err)
	}

	if !second.IdempotentReplay {
		t.Fatal("second result was not marked as an idempotent replay")
	}
	if second.Transfer.ID != first.Transfer.ID {
		t.Fatalf("replay transfer id = %s, want %s", second.Transfer.ID, first.Transfer.ID)
	}
	if len(second.LedgerEntries) != 2 {
		t.Fatalf("replay ledger entry count = %d, want 2", len(second.LedgerEntries))
	}

	assertWalletBalance(t, transferService, "wallet_1", 875)
	assertWalletBalance(t, transferService, "wallet_2", 125)
}

func TestCreateTransferRejectsIdempotencyKeyWithDifferentPayload(t *testing.T) {
	ctx := context.Background()
	transferService := newTestService(t)
	mustCreateWallet(t, transferService, "wallet_1", 1_000)
	mustCreateWallet(t, transferService, "wallet_2", 0)

	_, err := transferService.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "key-conflict",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}

	_, err = transferService.CreateTransfer(ctx, domain.CreateTransferInput{
		IdempotencyKey: "key-conflict",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	})
	assertAppErrorCode(t, err, domain.ErrorIdempotencyConflict)

	assertWalletBalance(t, transferService, "wallet_1", 900)
	assertWalletBalance(t, transferService, "wallet_2", 100)
}

func TestCreateTransferInsufficientFundsIsDurableFailure(t *testing.T) {
	ctx := context.Background()
	transferService := newTestService(t)
	mustCreateWallet(t, transferService, "wallet_1", 50)
	mustCreateWallet(t, transferService, "wallet_2", 0)

	input := domain.CreateTransferInput{
		IdempotencyKey: "key-insufficient",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
	first, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}
	if first.Transfer.State != domain.TransferFailed {
		t.Fatalf("transfer state = %s, want %s", first.Transfer.State, domain.TransferFailed)
	}
	if first.Transfer.ErrorCode != domain.ErrorInsufficientFunds {
		t.Fatalf("error code = %s, want %s", first.Transfer.ErrorCode, domain.ErrorInsufficientFunds)
	}
	if len(first.LedgerEntries) != 0 {
		t.Fatalf("ledger entry count = %d, want 0", len(first.LedgerEntries))
	}

	second, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("replay CreateTransfer returned error: %v", err)
	}
	if second.Transfer.ID != first.Transfer.ID {
		t.Fatalf("replay transfer id = %s, want %s", second.Transfer.ID, first.Transfer.ID)
	}

	assertWalletBalance(t, transferService, "wallet_1", 50)
	assertWalletBalance(t, transferService, "wallet_2", 0)
}

func TestCreateTransferMissingDestinationIsDurableFailure(t *testing.T) {
	ctx := context.Background()
	transferService := newTestService(t)
	mustCreateWallet(t, transferService, "wallet_1", 50)

	input := domain.CreateTransferInput{
		IdempotencyKey: "key-missing-destination",
		FromWalletID:   "wallet_1",
		ToWalletID:     "missing_wallet",
		Amount:         25,
	}
	first, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}
	if first.Transfer.State != domain.TransferFailed {
		t.Fatalf("transfer state = %s, want %s", first.Transfer.State, domain.TransferFailed)
	}
	if first.Transfer.ErrorCode != domain.ErrorDestinationWalletNotFound {
		t.Fatalf("error code = %s, want %s", first.Transfer.ErrorCode, domain.ErrorDestinationWalletNotFound)
	}
	if len(first.LedgerEntries) != 0 {
		t.Fatalf("ledger entry count = %d, want 0", len(first.LedgerEntries))
	}

	second, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("replay CreateTransfer returned error: %v", err)
	}
	if second.Transfer.ID != first.Transfer.ID {
		t.Fatalf("replay transfer id = %s, want %s", second.Transfer.ID, first.Transfer.ID)
	}

	assertWalletBalance(t, transferService, "wallet_1", 50)
}

func TestCreateTransferMissingSourceIsDurableFailure(t *testing.T) {
	ctx := context.Background()
	transferService := newTestService(t)
	mustCreateWallet(t, transferService, "wallet_2", 50)

	input := domain.CreateTransferInput{
		IdempotencyKey: "key-missing-source",
		FromWalletID:   "missing_wallet",
		ToWalletID:     "wallet_2",
		Amount:         25,
	}
	first, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}
	if first.Transfer.State != domain.TransferFailed {
		t.Fatalf("transfer state = %s, want %s", first.Transfer.State, domain.TransferFailed)
	}
	if first.Transfer.ErrorCode != domain.ErrorSourceWalletNotFound {
		t.Fatalf("error code = %s, want %s", first.Transfer.ErrorCode, domain.ErrorSourceWalletNotFound)
	}
	if len(first.LedgerEntries) != 0 {
		t.Fatalf("ledger entry count = %d, want 0", len(first.LedgerEntries))
	}

	second, err := transferService.CreateTransfer(ctx, input)
	if err != nil {
		t.Fatalf("replay CreateTransfer returned error: %v", err)
	}
	if second.Transfer.ID != first.Transfer.ID {
		t.Fatalf("replay transfer id = %s, want %s", second.Transfer.ID, first.Transfer.ID)
	}

	assertWalletBalance(t, transferService, "wallet_2", 50)
}

func TestConcurrentTransfersDoNotOverspend(t *testing.T) {
	ctx := context.Background()
	transferService := newTestService(t)
	mustCreateWallet(t, transferService, "source", 100)
	mustCreateWallet(t, transferService, "destination", 0)

	const transferCount = 10
	const amount = 40

	start := make(chan struct{})
	results := make([]domain.TransferResult, transferCount)
	errs := make([]error, transferCount)
	var wg sync.WaitGroup

	for i := 0; i < transferCount; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			<-start
			results[index], errs[index] = transferService.CreateTransfer(ctx, domain.CreateTransferInput{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", index),
				FromWalletID:   "source",
				ToWalletID:     "destination",
				Amount:         amount,
			})
		}(i)
	}

	close(start)
	wg.Wait()

	processed := 0
	failed := 0
	for i := 0; i < transferCount; i++ {
		if errs[i] != nil {
			t.Fatalf("transfer %d returned error: %v", i, errs[i])
		}

		switch results[i].Transfer.State {
		case domain.TransferProcessed:
			processed++
			if len(results[i].LedgerEntries) != 2 {
				t.Fatalf("processed transfer %d ledger count = %d, want 2", i, len(results[i].LedgerEntries))
			}
		case domain.TransferFailed:
			failed++
			if results[i].Transfer.ErrorCode != domain.ErrorInsufficientFunds {
				t.Fatalf("failed transfer %d error = %s, want %s", i, results[i].Transfer.ErrorCode, domain.ErrorInsufficientFunds)
			}
		default:
			t.Fatalf("transfer %d state = %s, want PROCESSED or FAILED", i, results[i].Transfer.State)
		}
	}

	if processed != 2 {
		t.Fatalf("processed transfers = %d, want 2", processed)
	}
	if failed != 8 {
		t.Fatalf("failed transfers = %d, want 8", failed)
	}

	assertWalletBalance(t, transferService, "source", 20)
	assertWalletBalance(t, transferService, "destination", 80)
}

func newTestService(t *testing.T) *service.TransferService {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "wallet.db")
	store, err := repository.OpenSQLite(ctx, fmt.Sprintf("file:%s?_busy_timeout=5000&_foreign_keys=on", dbPath))
	if err != nil {
		t.Fatalf("OpenSQLite returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("Close returned error: %v", err)
		}
	})

	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate returned error: %v", err)
	}

	return service.NewTransferService(store)
}

func mustCreateWallet(t *testing.T, transferService *service.TransferService, id string, balance int64) {
	t.Helper()

	_, err := transferService.CreateWallet(context.Background(), domain.CreateWalletInput{
		ID:             id,
		InitialBalance: balance,
	})
	if err != nil {
		t.Fatalf("CreateWallet(%s) returned error: %v", id, err)
	}
}

func assertWalletBalance(t *testing.T, transferService *service.TransferService, id string, want int64) {
	t.Helper()

	wallet, err := transferService.GetWallet(context.Background(), id)
	if err != nil {
		t.Fatalf("GetWallet(%s) returned error: %v", id, err)
	}
	if wallet.Balance != want {
		t.Fatalf("wallet %s balance = %d, want %d", id, wallet.Balance, want)
	}
}

func assertLedgerBalances(t *testing.T, entries []domain.LedgerEntry, amount int64) {
	t.Helper()

	var debitTotal int64
	var creditTotal int64
	for _, entry := range entries {
		switch entry.Type {
		case domain.LedgerDebit:
			debitTotal += entry.Amount
		case domain.LedgerCredit:
			creditTotal += entry.Amount
		default:
			t.Fatalf("unexpected ledger entry type %s", entry.Type)
		}
	}

	if debitTotal != amount {
		t.Fatalf("debit total = %d, want %d", debitTotal, amount)
	}
	if creditTotal != amount {
		t.Fatalf("credit total = %d, want %d", creditTotal, amount)
	}
}

func assertAppErrorCode(t *testing.T, err error, want domain.ErrorCode) {
	t.Helper()

	var appErr *domain.AppError
	if !errors.As(err, &appErr) {
		t.Fatalf("error = %v, want AppError with code %s", err, want)
	}
	if appErr.Code != want {
		t.Fatalf("error code = %s, want %s", appErr.Code, want)
	}
}
