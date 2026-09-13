package repository_test

import (
	"context"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/ankit729/wallet-transfer/internal/domain"
	"github.com/ankit729/wallet-transfer/internal/repository"
	"github.com/ankit729/wallet-transfer/internal/service"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestIdempotentTransfer(t *testing.T) {
	pool := openTestDatabase(t)
	defer pool.Close()

	store := repository.New(pool)
	applicationService := service.New(store, nil, nil)

	sourceID := "source-" + uuid.NewString()
	destinationID := "destination-" + uuid.NewString()

	createTestWallet(t, applicationService, sourceID, 100)
	createTestWallet(t, applicationService, destinationID, 0)

	request := domain.TransferRequest{
		IdempotencyKey: "idempotency-" + uuid.NewString(),
		FromWalletID:   sourceID,
		ToWalletID:     destinationID,
		Amount:         40,
	}

	first, err := applicationService.CreateTransfer(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	second, err := applicationService.CreateTransfer(
		context.Background(),
		request,
	)
	if err != nil {
		t.Fatalf("second transfer failed: %v", err)
	}

	if first.ResponseCode != 201 {
		t.Fatalf("expected first response code 201, got %d", first.ResponseCode)
	}

	if second.ResponseCode != 201 {
		t.Fatalf("expected second response code 201, got %d", second.ResponseCode)
	}

	if first.ID != second.ID {
		t.Fatalf(
			"expected same transfer ID, got %s and %s",
			first.ID,
			second.ID,
		)
	}

	if !second.Replayed {
		t.Fatal("expected second request to be marked as replayed")
	}

	if string(first.ResponseBody) != string(second.ResponseBody) {
		t.Fatal("expected identical response bodies")
	}

	source, err := applicationService.GetWallet(
		context.Background(),
		sourceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	destination, err := applicationService.GetWallet(
		context.Background(),
		destinationID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if source.Balance != 60 {
		t.Fatalf("expected source balance 60, got %d", source.Balance)
	}

	if destination.Balance != 40 {
		t.Fatalf("expected destination balance 40, got %d", destination.Balance)
	}

	ledgerCount, err := store.CountLedgerEntries(
		context.Background(),
		first.ID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", ledgerCount)
	}
}

func TestConcurrentTransfersDoNotDoubleSpend(t *testing.T) {
	pool := openTestDatabase(t)
	defer pool.Close()

	store := repository.New(pool)
	applicationService := service.New(store, nil, nil)

	sourceID := "source-" + uuid.NewString()
	destinationOneID := "destination-one-" + uuid.NewString()
	destinationTwoID := "destination-two-" + uuid.NewString()

	createTestWallet(t, applicationService, sourceID, 100)
	createTestWallet(t, applicationService, destinationOneID, 0)
	createTestWallet(t, applicationService, destinationTwoID, 0)

	requests := []domain.TransferRequest{
		{
			IdempotencyKey: "concurrent-one-" + uuid.NewString(),
			FromWalletID:   sourceID,
			ToWalletID:     destinationOneID,
			Amount:         70,
		},
		{
			IdempotencyKey: "concurrent-two-" + uuid.NewString(),
			FromWalletID:   sourceID,
			ToWalletID:     destinationTwoID,
			Amount:         70,
		},
	}

	results := make(chan domain.TransferResult, len(requests))
	errs := make(chan error, len(requests))

	var waitGroup sync.WaitGroup
	waitGroup.Add(len(requests))

	for _, request := range requests {
		request := request

		go func() {
			defer waitGroup.Done()

			result, err := applicationService.CreateTransfer(
				context.Background(),
				request,
			)
			if err != nil {
				errs <- err
				return
			}

			results <- result
		}()
	}

	waitGroup.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent transfer returned unexpected error: %v", err)
	}

	var processed int
	var failed int
	var processedTransferID string

	for result := range results {
		switch result.ResponseCode {
		case 201:
			processed++
			processedTransferID = result.ID
		case 422:
			failed++
		default:
			t.Fatalf(
				"unexpected response code: %d",
				result.ResponseCode,
			)
		}
	}

	if processed != 1 {
		t.Fatalf("expected exactly one processed transfer, got %d", processed)
	}

	if failed != 1 {
		t.Fatalf("expected exactly one failed transfer, got %d", failed)
	}

	source, err := applicationService.GetWallet(
		context.Background(),
		sourceID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if source.Balance != 30 {
		t.Fatalf("expected source balance 30, got %d", source.Balance)
	}

	ledgerCount, err := store.CountLedgerEntries(
		context.Background(),
		processedTransferID,
	)
	if err != nil {
		t.Fatal(err)
	}

	if ledgerCount != 2 {
		t.Fatalf("expected 2 ledger entries, got %d", ledgerCount)
	}
}

func createTestWallet(
	t *testing.T,
	applicationService *service.Service,
	id string,
	balance int64,
) {
	t.Helper()

	_, err := applicationService.CreateWallet(
		context.Background(),
		domain.CreateWalletRequest{
			ID:             id,
			Currency:       "USD",
			InitialBalance: balance,
		},
	)
	if err != nil {
		t.Fatalf("failed to create wallet: %v", err)
	}
}

func openTestDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set")
	}

	pool, err := pgxpool.New(
		context.Background(),
		databaseURL,
	)
	if err != nil {
		t.Fatalf("failed to create test pool: %v", err)
	}

	ctx, cancel := context.WithTimeout(
		context.Background(),
		5*time.Second,
	)
	defer cancel()

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("failed to ping test database: %v", err)
	}

	return pool
}
