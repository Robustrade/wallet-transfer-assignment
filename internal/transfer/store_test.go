package transfer

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "test.db")+"?_pragma=foreign_keys(1)&_pragma=busy_timeout(30000)&_txlock=immediate")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	store := NewStore(db)
	if err := store.Initialize(); err != nil {
		t.Fatal(err)
	}
	return store
}

func seedWallets(t *testing.T, store *Store) {
	t.Helper()
	if err := store.CreateWallet(context.Background(), "source", 100); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWallet(context.Background(), "destination", 0); err != nil {
		t.Fatal(err)
	}
}

func TestGetAllWalletsReturnsWalletsByID(t *testing.T) {
	store := testStore(t)
	if err := store.CreateWallet(context.Background(), "wallet-b", 200); err != nil {
		t.Fatal(err)
	}
	if err := store.CreateWallet(context.Background(), "wallet-a", 100); err != nil {
		t.Fatal(err)
	}

	wallets, err := store.GetAllWallets(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(wallets) != 2 || wallets[0].ID != "wallet-a" || wallets[1].ID != "wallet-b" || wallets[0].BalanceCents != 100 || wallets[1].BalanceCents != 200 {
		t.Fatalf("wallets = %#v", wallets)
	}
}

func TestTransferCreatesBalancedLedgerAndUpdatesBalances(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)

	result, created, err := store.Transfer(context.Background(), TransferRequest{
		IdempotencyKey: "request-1",
		FromWalletID:   "source",
		ToWalletID:     "destination",
		AmountCents:    40,
	})
	if err != nil || !created || result.Status != "PROCESSED" {
		t.Fatalf("transfer = %#v, created = %v, err = %v", result, created, err)
	}

	entries, err := store.LedgerEntries(context.Background(), result.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 ||
		entries[0].TransferID != result.ID || entries[1].TransferID != result.ID ||
		entries[0].WalletID != "source" || entries[1].WalletID != "destination" ||
		entries[0].EntryType != "DEBIT" || entries[1].EntryType != "CREDIT" ||
		entries[0].AmountCents != 40 || entries[1].AmountCents != 40 {
		t.Fatalf("unexpected ledger entries: %#v", entries)
	}

	source, _ := store.GetWallet(context.Background(), "source")
	destination, _ := store.GetWallet(context.Background(), "destination")
	if source.BalanceCents != 60 || destination.BalanceCents != 40 {
		t.Fatalf("unexpected balances: source=%d destination=%d", source.BalanceCents, destination.BalanceCents)
	}
}

func TestGetAllLedgerEntriesReturnsEntriesByCreationOrder(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)

	first, _, err := store.Transfer(context.Background(), TransferRequest{
		IdempotencyKey: "request-1",
		FromWalletID:   "source",
		ToWalletID:     "destination",
		AmountCents:    10,
	})
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := store.Transfer(context.Background(), TransferRequest{
		IdempotencyKey: "request-2",
		FromWalletID:   "source",
		ToWalletID:     "destination",
		AmountCents:    20,
	})
	if err != nil {
		t.Fatal(err)
	}

	entries, err := store.GetAllLedgerEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 4 {
		t.Fatalf("ledger entries = %#v, want 4 entries", entries)
	}
	if entries[0].TransferID != first.ID || entries[1].TransferID != first.ID || entries[2].TransferID != second.ID || entries[3].TransferID != second.ID {
		t.Fatalf("ledger transfer IDs = %#v", entries)
	}
}

func TestTransferIsIdempotentAndRejectsChangedPayload(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)
	request := TransferRequest{IdempotencyKey: "request-1", FromWalletID: "source", ToWalletID: "destination", AmountCents: 40}

	first, created, err := store.Transfer(context.Background(), request)
	if err != nil || !created {
		t.Fatal(err)
	}
	second, created, err := store.Transfer(context.Background(), request)
	if err != nil || created || second.ID != first.ID {
		t.Fatalf("replay = %#v, created = %v, err = %v", second, created, err)
	}
	_, _, err = store.Transfer(context.Background(), TransferRequest{IdempotencyKey: "request-1", FromWalletID: "source", ToWalletID: "destination", AmountCents: 41})
	if !errors.Is(err, ErrIdempotencyKeyReuse) {
		t.Fatal("expected changed idempotency payload to fail")
	}
}

func TestConcurrentTransfersCannotDoubleSpend(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)

	var wait sync.WaitGroup
	results := make(chan error, 2)
	for _, key := range []string{"request-1", "request-2"} {
		wait.Add(1)
		go func(idempotencyKey string) {
			defer wait.Done()
			_, _, err := store.Transfer(context.Background(), TransferRequest{IdempotencyKey: idempotencyKey, FromWalletID: "source", ToWalletID: "destination", AmountCents: 70})
			results <- err
		}(key)
	}
	wait.Wait()
	close(results)

	processed := 0
	for err := range results {
		if err == nil {
			processed++
		} else if !errors.Is(err, ErrInsufficientFunds) {
			t.Fatalf("unexpected concurrent transfer error: %v", err)
		}
	}
	if processed != 1 {
		t.Fatalf("processed transfers = %d, want 1", processed)
	}
	wallet, _ := store.GetWallet(context.Background(), "source")
	if wallet.BalanceCents != 30 {
		t.Fatalf("source balance = %d, want 30", wallet.BalanceCents)
	}
}

func TestConcurrentDuplicateRequestsCreateOneTransfer(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)

	var wait sync.WaitGroup
	results := make(chan struct {
		transfer Transfer
		created  bool
		err      error
	}, 2)
	request := TransferRequest{IdempotencyKey: "same-request", FromWalletID: "source", ToWalletID: "destination", AmountCents: 40}
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			transfer, created, err := store.Transfer(context.Background(), request)
			results <- struct {
				transfer Transfer
				created  bool
				err      error
			}{transfer, created, err}
		}()
	}
	wait.Wait()
	close(results)

	var first Transfer
	createdCount := 0
	for result := range results {
		if result.err != nil {
			t.Fatalf("duplicate request error: %v", result.err)
		}
		if result.created {
			createdCount++
		}
		if first.ID == "" {
			first = result.transfer
		} else if result.transfer.ID != first.ID {
			t.Fatalf("duplicate transfer IDs = %q and %q", first.ID, result.transfer.ID)
		}
	}
	if createdCount != 1 {
		t.Fatalf("created transfers = %d, want 1", createdCount)
	}

	entries, err := store.GetAllLedgerEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(entries))
	}
}

func TestConcurrentTransfersFromDifferentWallets(t *testing.T) {
	store := testStore(t)
	transferCount := stressTestValue(t, "TRANSFER_STRESS_COUNT", 1000)
	workerCount := stressTestValue(t, "TRANSFER_STRESS_WORKERS", 16)
	if transferCount <= 0 || workerCount <= 0 {
		t.Fatal("TRANSFER_STRESS_COUNT and TRANSFER_STRESS_WORKERS must be positive")
	}

	for index := 0; index < transferCount; index++ {
		if err := store.CreateWallet(context.Background(), fmt.Sprintf("source-%04d", index), 100); err != nil {
			t.Fatal(err)
		}
		if err := store.CreateWallet(context.Background(), fmt.Sprintf("destination-%04d", index), 0); err != nil {
			t.Fatal(err)
		}
	}

	var wait sync.WaitGroup
	results := make(chan error, transferCount)
	jobs := make(chan int)
	for worker := 0; worker < workerCount; worker++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for index := range jobs {
				_, created, err := store.Transfer(context.Background(), TransferRequest{
					IdempotencyKey: fmt.Sprintf("stress-request-%04d", index),
					FromWalletID:   fmt.Sprintf("source-%04d", index),
					ToWalletID:     fmt.Sprintf("destination-%04d", index),
					AmountCents:    40,
				})
				if err == nil && !created {
					err = errors.New("new transfer was reported as a replay")
				}
				results <- err
			}
		}()
	}
	for index := 0; index < transferCount; index++ {
		jobs <- index
	}
	close(jobs)
	wait.Wait()
	close(results)

	for err := range results {
		if err != nil {
			t.Fatalf("concurrent transfer failed: %v", err)
		}
	}

	entries, err := store.GetAllLedgerEntries(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != transferCount*2 {
		t.Fatalf("ledger entries = %d, want %d", len(entries), transferCount*2)
	}

	for index := 0; index < transferCount; index++ {
		source, err := store.GetWallet(context.Background(), fmt.Sprintf("source-%04d", index))
		if err != nil {
			t.Fatal(err)
		}
		destination, err := store.GetWallet(context.Background(), fmt.Sprintf("destination-%04d", index))
		if err != nil {
			t.Fatal(err)
		}
		if source.BalanceCents != 60 || destination.BalanceCents != 40 {
			t.Fatalf("wallet %d balances = source:%d destination:%d, want source:60 destination:40", index, source.BalanceCents, destination.BalanceCents)
		}
	}
}

func stressTestValue(t *testing.T, name string, defaultValue int) int {
	t.Helper()
	value := os.Getenv(name)
	if value == "" {
		return defaultValue
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		t.Fatalf("invalid %s=%q: %v", name, value, err)
	}
	return parsed
}

func TestFailedTransferIsStoredAndReplaySafe(t *testing.T) {
	store := testStore(t)
	seedWallets(t, store)
	request := TransferRequest{IdempotencyKey: "request-1", FromWalletID: "source", ToWalletID: "destination", AmountCents: 101}

	first, _, err := store.Transfer(context.Background(), request)
	if !errors.Is(err, ErrInsufficientFunds) || first.Status != "FAILED" {
		t.Fatalf("failed transfer = %#v, err = %v", first, err)
	}
	second, created, err := store.Transfer(context.Background(), request)
	if err != nil || created || second.ID != first.ID || second.Status != "FAILED" {
		t.Fatalf("failed replay = %#v, created = %v, err = %v", second, created, err)
	}
	entries, _ := store.LedgerEntries(context.Background(), first.ID)
	if len(entries) != 0 {
		t.Fatalf("failed transfer has ledger entries: %#v", entries)
	}
	source, _ := store.GetWallet(context.Background(), "source")
	destination, _ := store.GetWallet(context.Background(), "destination")
	if source.BalanceCents != 100 || destination.BalanceCents != 0 {
		t.Fatalf("failed transfer changed balances: source=%d destination=%d", source.BalanceCents, destination.BalanceCents)
	}
}
