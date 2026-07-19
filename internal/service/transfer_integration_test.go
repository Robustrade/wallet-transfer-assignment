//go:build integration

package service

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/postgres"
)

func TestTransferServiceIntegrationIdempotentReplay(t *testing.T) {
	store := newIntegrationStore(t)
	service := NewTransferService(store)

	request := CreateTransferRequest{
		IdempotencyKey: "integration-replay-key",
		FromWalletID:   testFromWalletID,
		ToWalletID:     testToWalletID,
		Amount:         100,
	}

	first, err := service.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}
	second, err := service.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatalf("second CreateTransfer returned error: %v", err)
	}

	if !second.Replayed {
		t.Fatal("second request was not replayed")
	}
	if second.Transfer != first.Transfer {
		t.Fatalf("replayed transfer differs: got %+v want %+v", second.Transfer, first.Transfer)
	}
	assertLedgerCount(t, store, first.Transfer.TransferID, 2)
}

func TestTransferServiceIntegrationConcurrentDebits(t *testing.T) {
	store := newIntegrationStore(t)
	service := NewTransferService(store)

	const transferCount = 20
	var wg sync.WaitGroup
	results := make(chan CreateTransferResult, transferCount)
	errs := make(chan error, transferCount)

	for index := range transferCount {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			result, err := service.CreateTransfer(context.Background(), CreateTransferRequest{
				IdempotencyKey: "integration-concurrent-key-" + string(rune('a'+index)),
				FromWalletID:   testFromWalletID,
				ToWalletID:     testToWalletID,
				Amount:         1000,
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}(index)
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}

	processed := 0
	failed := 0
	for result := range results {
		switch result.Transfer.Status {
		case string(domain.TransferStatusProcessed):
			processed++
		case string(domain.TransferStatusFailed):
			failed++
		default:
			t.Fatalf("unexpected transfer status %q", result.Transfer.Status)
		}
	}

	if processed != 10 {
		t.Fatalf("processed count = %d, want 10", processed)
	}
	if failed != 10 {
		t.Fatalf("failed count = %d, want 10", failed)
	}

	fromWallet, err := store.GetWallet(context.Background(), testFromWalletID)
	if err != nil {
		t.Fatalf("GetWallet returned error: %v", err)
	}
	if fromWallet.Balance != 0 {
		t.Fatalf("from wallet balance = %d, want 0", fromWallet.Balance)
	}
}

func newIntegrationStore(t *testing.T) *postgres.Store {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is required for integration tests")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	pool, err := postgres.Connect(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect database: %v", err)
	}
	t.Cleanup(pool.Close)

	for _, migration := range []string{"999_down.sql", "001_init.sql", "002_seed.sql"} {
		contents, err := os.ReadFile(filepath.Join("..", "..", "migrations", migration))
		if err != nil {
			t.Fatalf("read migration %s: %v", migration, err)
		}
		if _, err := pool.Exec(ctx, string(contents)); err != nil {
			t.Fatalf("apply migration %s: %v", migration, err)
		}
	}

	return postgres.NewStore(pool)
}

func assertLedgerCount(t *testing.T, store *postgres.Store, transferID string, expected int) {
	t.Helper()

	fromEntries, err := store.ListWalletLedger(context.Background(), testFromWalletID, 100, 0)
	if err != nil {
		t.Fatalf("ListWalletLedger for source returned error: %v", err)
	}
	toEntries, err := store.ListWalletLedger(context.Background(), testToWalletID, 100, 0)
	if err != nil {
		t.Fatalf("ListWalletLedger for destination returned error: %v", err)
	}

	count := 0
	for _, entry := range append(fromEntries, toEntries...) {
		if entry.TransferID == transferID {
			count++
		}
	}
	if count != expected {
		t.Fatalf("ledger entry count = %d, want %d", count, expected)
	}
}
