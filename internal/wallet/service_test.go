package wallet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
)

func newTestService(t *testing.T) (*Service, *Repository) {
	t.Helper()
	repository, err := OpenRepository(t.TempDir() + "/wallets.sqlite3")
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Errorf("close repository: %v", err)
		}
	})
	return NewService(repository), repository
}

func mustCreateWallet(t *testing.T, service *Service, walletID string, balance string) {
	t.Helper()
	var raw json.RawMessage
	if balance != "" {
		raw = json.RawMessage(balance)
	}
	_, err := service.CreateWallet(context.Background(), CreateWalletRequest{
		WalletID:       &walletID,
		InitialBalance: raw,
	})
	if err != nil {
		t.Fatalf("create wallet %q: %v", walletID, err)
	}
}

func testTransfer(key, to string, amount Money) TransferRequest {
	return TransferRequest{
		IdempotencyKey: key,
		FromWalletID:   "source",
		ToWalletID:     to,
		Amount:         amount,
	}
}

func TestParseMoney(t *testing.T) {
	tests := []struct {
		input   string
		allow0  bool
		want    Money
		wantErr bool
	}{
		{input: "0", allow0: true, want: 0},
		{input: "12.5", want: 1250},
		{input: "1.230", want: 123},
		{input: "1e2", want: 10000},
		{input: "0.001", wantErr: true},
		{input: "-1", wantErr: true},
		{input: `"1.25"`, wantErr: true},
		{input: "92233720368547758.08", wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.input, func(t *testing.T) {
			got, err := ParseMoney([]byte(test.input), test.allow0)
			if (err != nil) != test.wantErr {
				t.Fatalf("ParseMoney(%s) error = %v, wantErr %v", test.input, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Fatalf("ParseMoney(%s) = %d, want %d", test.input, got, test.want)
			}
		})
	}
}

func TestProcessedTransferUpdatesBalancesAndCreatesBalancedLedger(t *testing.T) {
	service, repository := newTestService(t)
	mustCreateWallet(t, service, "source", "100")
	mustCreateWallet(t, service, "destination", "10")

	result, err := service.CreateTransfer(context.Background(), testTransfer("transfer-1", "destination", 2500))
	if err != nil {
		t.Fatalf("create transfer: %v", err)
	}
	if result.Status != "PROCESSED" || result.FailureCode != nil {
		t.Fatalf("unexpected result: %+v", result)
	}
	source, err := service.GetWallet(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	destination, err := service.GetWallet(context.Background(), "destination")
	if err != nil {
		t.Fatal(err)
	}
	if source.Balance != 7500 || destination.Balance != 3500 {
		t.Fatalf("balances = %d, %d; want 7500, 3500", source.Balance, destination.Balance)
	}

	var entries int
	var debits, credits int64
	err = repository.db.QueryRow(
		`SELECT COUNT(*), SUM(CASE WHEN entry_type = 'DEBIT' THEN amount_minor ELSE 0 END),
			SUM(CASE WHEN entry_type = 'CREDIT' THEN amount_minor ELSE 0 END)
		 FROM ledger_entries WHERE transfer_id = ?`,
		result.TransferID,
	).Scan(&entries, &debits, &credits)
	if err != nil {
		t.Fatal(err)
	}
	if entries != 2 || debits != 2500 || credits != 2500 {
		t.Fatalf("ledger = count %d, debit %d, credit %d", entries, debits, credits)
	}
}

func TestIdempotentReplayAndConflictingPayload(t *testing.T) {
	service, _ := newTestService(t)
	mustCreateWallet(t, service, "source", "100")
	mustCreateWallet(t, service, "destination", "0")

	first, err := service.CreateTransfer(context.Background(), testTransfer("same-key", "destination", 3000))
	if err != nil {
		t.Fatal(err)
	}
	replayed, err := service.CreateTransfer(context.Background(), testTransfer("same-key", "destination", 3000))
	if err != nil {
		t.Fatal(err)
	}
	if first.TransferID != replayed.TransferID || first.Status != replayed.Status {
		t.Fatalf("replay did not return original result: %+v, %+v", first, replayed)
	}
	_, err = service.CreateTransfer(context.Background(), testTransfer("same-key", "destination", 2500))
	if !errors.Is(err, ErrIdempotencyReuse) {
		t.Fatalf("error = %v, want ErrIdempotencyReuse", err)
	}
	source, err := service.GetWallet(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	if source.Balance != 7000 {
		t.Fatalf("source balance = %d, want 7000", source.Balance)
	}
}

func TestInsufficientFundsAreRecordedAndReplayed(t *testing.T) {
	service, repository := newTestService(t)
	mustCreateWallet(t, service, "source", "10")
	mustCreateWallet(t, service, "destination", "0")

	request := testTransfer("insufficient", "destination", 2000)
	first, err := service.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.CreateTransfer(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if first.Status != "FAILED" || first.FailureCode == nil || *first.FailureCode != "INSUFFICIENT_FUNDS" {
		t.Fatalf("unexpected failure response: %+v", first)
	}
	if first.TransferID != replay.TransferID {
		t.Fatalf("retry created a new transfer: %s != %s", first.TransferID, replay.TransferID)
	}
	var entries int
	if err := repository.db.QueryRow(
		"SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?",
		first.TransferID,
	).Scan(&entries); err != nil {
		t.Fatal(err)
	}
	if entries != 0 {
		t.Fatalf("failed transfer has %d ledger entries", entries)
	}
}

func TestDestinationBalanceOverflowFailsWithoutChangingBalances(t *testing.T) {
	service, _ := newTestService(t)
	mustCreateWallet(t, service, "source", "0.01")
	mustCreateWallet(t, service, "destination", "92233720368547758.07")

	result, err := service.CreateTransfer(
		context.Background(),
		testTransfer("overflow", "destination", 1),
	)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != "FAILED" || result.FailureCode == nil || *result.FailureCode != "BALANCE_LIMIT_EXCEEDED" {
		t.Fatalf("unexpected result: %+v", result)
	}
	source, err := service.GetWallet(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	if source.Balance != 1 {
		t.Fatalf("source balance = %d, want 1", source.Balance)
	}
}

func TestOpeningBalanceCreatesCreditEntry(t *testing.T) {
	service, repository := newTestService(t)
	mustCreateWallet(t, service, "funded", "12.34")

	var entryType string
	var amount int64
	if err := repository.db.QueryRow(
		"SELECT entry_type, amount_minor FROM ledger_entries WHERE wallet_id = ? AND transfer_id IS NULL",
		"funded",
	).Scan(&entryType, &amount); err != nil {
		t.Fatal(err)
	}
	if entryType != "CREDIT" || amount != 1234 {
		t.Fatalf("opening entry = %s/%d, want CREDIT/1234", entryType, amount)
	}
}

func TestInMemoryRepositoryRetainsDataAcrossConnections(t *testing.T) {
	repository, err := OpenRepository(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	service := NewService(repository)
	mustCreateWallet(t, service, "memory-wallet", "8")

	wallet, err := service.GetWallet(context.Background(), "memory-wallet")
	if err != nil {
		t.Fatal(err)
	}
	if wallet.Balance != 800 {
		t.Fatalf("balance = %d, want 800", wallet.Balance)
	}
}

func TestLedgerEntriesAreImmutable(t *testing.T) {
	service, repository := newTestService(t)
	mustCreateWallet(t, service, "source", "10")
	mustCreateWallet(t, service, "destination", "0")
	result, err := service.CreateTransfer(context.Background(), testTransfer("immutable", "destination", 100))
	if err != nil {
		t.Fatal(err)
	}

	_, err = repository.db.Exec(
		"UPDATE ledger_entries SET amount_minor = 200 WHERE transfer_id = ?",
		result.TransferID,
	)
	if err == nil {
		t.Fatal("ledger entry update succeeded, want immutable-ledger constraint error")
	}
}

func TestInvalidRequestsAndMissingWallets(t *testing.T) {
	service, _ := newTestService(t)
	mustCreateWallet(t, service, "source", "10")

	_, err := service.CreateTransfer(context.Background(), testTransfer("missing", "absent", 100))
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing wallet error = %v, want ErrNotFound", err)
	}
	_, err = service.CreateTransfer(context.Background(), testTransfer("self", "source", 100))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("self-transfer error = %v, want ErrInvalidRequest", err)
	}
	_, err = service.CreateTransfer(context.Background(), testTransfer("zero", "other", 0))
	if !errors.Is(err, ErrInvalidRequest) {
		t.Fatalf("zero amount error = %v, want ErrInvalidRequest", err)
	}
	_, err = service.CreateWallet(context.Background(), CreateWalletRequest{
		WalletID:       stringPointer("source"),
		InitialBalance: json.RawMessage("0"),
	})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate wallet error = %v, want ErrConflict", err)
	}
}

func TestConcurrentDebitsCannotOverdrawWallet(t *testing.T) {
	service, repository := newTestService(t)
	mustCreateWallet(t, service, "source", "100")
	mustCreateWallet(t, service, "destination-a", "0")
	mustCreateWallet(t, service, "destination-b", "0")

	var wait sync.WaitGroup
	results := make(chan TransferResult, 2)
	errorsFound := make(chan error, 2)
	for index, destination := range []string{"destination-a", "destination-b"} {
		wait.Add(1)
		go func(index int, destination string) {
			defer wait.Done()
			result, err := service.CreateTransfer(
				context.Background(),
				testTransfer(fmt.Sprintf("debit-%d", index), destination, 8000),
			)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}(index, destination)
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}
	statuses := map[string]int{}
	for result := range results {
		statuses[result.Status]++
	}
	if statuses["PROCESSED"] != 1 || statuses["FAILED"] != 1 {
		t.Fatalf("transfer statuses = %v, want one processed and one failed", statuses)
	}
	source, err := service.GetWallet(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	if source.Balance != 2000 {
		t.Fatalf("source balance = %d, want 2000", source.Balance)
	}
	var processedEntries int
	if err := repository.db.QueryRow(
		`SELECT COUNT(*) FROM ledger_entries
		 WHERE transfer_id IN (SELECT transfer_id FROM transfers WHERE status = 'PROCESSED')`,
	).Scan(&processedEntries); err != nil {
		t.Fatal(err)
	}
	if processedEntries != 2 {
		t.Fatalf("processed ledger entries = %d, want 2", processedEntries)
	}
}

func TestConcurrentRetriesWithSameKeyCreateSingleTransfer(t *testing.T) {
	service, repository := newTestService(t)
	mustCreateWallet(t, service, "source", "100")
	mustCreateWallet(t, service, "destination", "0")

	const attempts = 8
	var wait sync.WaitGroup
	results := make(chan TransferResult, attempts)
	errorsFound := make(chan error, attempts)
	for range attempts {
		wait.Add(1)
		go func() {
			defer wait.Done()
			result, err := service.CreateTransfer(
				context.Background(),
				testTransfer("parallel-retry", "destination", 2500),
			)
			if err != nil {
				errorsFound <- err
				return
			}
			results <- result
		}()
	}
	wait.Wait()
	close(results)
	close(errorsFound)
	for err := range errorsFound {
		t.Fatal(err)
	}

	var transferID string
	count := 0
	for result := range results {
		if transferID == "" {
			transferID = result.TransferID
		}
		if result.TransferID != transferID {
			t.Fatalf("concurrent retry returned %s, expected %s", result.TransferID, transferID)
		}
		count++
	}
	if count != attempts {
		t.Fatalf("received %d results, want %d", count, attempts)
	}
	var transferCount int
	if err := repository.db.QueryRow(
		"SELECT COUNT(*) FROM transfers WHERE idempotency_key = ?",
		"parallel-retry",
	).Scan(&transferCount); err != nil {
		t.Fatal(err)
	}
	if transferCount != 1 {
		t.Fatalf("transfer count = %d, want 1", transferCount)
	}
	source, err := service.GetWallet(context.Background(), "source")
	if err != nil {
		t.Fatal(err)
	}
	if source.Balance != 7500 {
		t.Fatalf("source balance = %d, want 7500", source.Balance)
	}
}

func stringPointer(value string) *string {
	return &value
}
