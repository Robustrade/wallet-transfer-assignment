package service_test

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sync"
	"testing"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
	"wallet-transfer-assignment/internal/store"
)

func TestCreateTransferProcessesLedgerAndBalancesAtomically(t *testing.T) {
	ctx := context.Background()
	repository, transferService := newTestService(t)
	createWallets(t, ctx, repository, map[string]int64{
		"wallet_1": 500,
		"wallet_2": 25,
	})

	outcome, err := transferService.CreateTransfer(ctx, domain.TransferCommand{
		IdempotencyKey: "key-success",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer returned error: %v", err)
	}
	if outcome.StatusCode != http.StatusCreated {
		t.Fatalf("status code = %d, want %d", outcome.StatusCode, http.StatusCreated)
	}

	response := requireTransferResponse(t, outcome)
	if response.Transfer.State != domain.TransferProcessed {
		t.Fatalf("transfer state = %s, want %s", response.Transfer.State, domain.TransferProcessed)
	}
	if len(response.LedgerEntries) != 2 {
		t.Fatalf("ledger entries = %d, want 2", len(response.LedgerEntries))
	}
	assertBalancedLedger(t, response.LedgerEntries, 100)
	assertBalance(t, ctx, repository, "wallet_1", 400)
	assertBalance(t, ctx, repository, "wallet_2", 125)
}

func TestCreateTransferReplaysDuplicateIdempotencyKeyWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	repository, transferService := newTestService(t)
	createWallets(t, ctx, repository, map[string]int64{
		"wallet_1": 500,
		"wallet_2": 25,
	})
	command := domain.TransferCommand{
		IdempotencyKey: "key-duplicate",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := transferService.CreateTransfer(ctx, command)
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}
	second, err := transferService.CreateTransfer(ctx, command)
	if err != nil {
		t.Fatalf("second CreateTransfer returned error: %v", err)
	}

	firstResponse := requireTransferResponse(t, first)
	secondResponse := requireTransferResponse(t, second)
	if second.StatusCode != first.StatusCode {
		t.Fatalf("duplicate status code = %d, want %d", second.StatusCode, first.StatusCode)
	}
	if secondResponse.Transfer.ID != firstResponse.Transfer.ID {
		t.Fatalf("duplicate transfer ID = %q, want %q", secondResponse.Transfer.ID, firstResponse.Transfer.ID)
	}
	assertBalance(t, ctx, repository, "wallet_1", 400)
	assertBalance(t, ctx, repository, "wallet_2", 125)
}

func TestCreateTransferReplaysConcurrentDuplicateIdempotencyKeyWithoutSideEffects(t *testing.T) {
	ctx := context.Background()
	repository, transferService := newTestService(t)
	createWallets(t, ctx, repository, map[string]int64{
		"wallet_1": 500,
		"wallet_2": 25,
	})
	command := domain.TransferCommand{
		IdempotencyKey: "key-concurrent-duplicate",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	var wg sync.WaitGroup
	results := make(chan domain.Outcome, 2)
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outcome, err := transferService.CreateTransfer(ctx, command)
			if err != nil {
				errs <- err
				return
			}
			results <- outcome
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent duplicate CreateTransfer returned error: %v", err)
	}
	outcomes := make([]domain.Outcome, 0, 2)
	for outcome := range results {
		outcomes = append(outcomes, outcome)
	}
	if len(outcomes) != 2 {
		t.Fatalf("outcomes = %d, want 2", len(outcomes))
	}

	firstResponse := requireTransferResponse(t, outcomes[0])
	secondResponse := requireTransferResponse(t, outcomes[1])
	if outcomes[0].StatusCode != http.StatusCreated || outcomes[1].StatusCode != http.StatusCreated {
		t.Fatalf("status codes = %d/%d, want %d/%d", outcomes[0].StatusCode, outcomes[1].StatusCode, http.StatusCreated, http.StatusCreated)
	}
	if secondResponse.Transfer.ID != firstResponse.Transfer.ID {
		t.Fatalf("duplicate transfer ID = %q, want %q", secondResponse.Transfer.ID, firstResponse.Transfer.ID)
	}
	assertLedgerCount(t, ctx, repository, firstResponse.Transfer.ID, 2)
	assertBalance(t, ctx, repository, "wallet_1", 400)
	assertBalance(t, ctx, repository, "wallet_2", 125)
}

func TestCreateTransferRejectsIdempotencyKeyReuseWithDifferentRequest(t *testing.T) {
	ctx := context.Background()
	repository, transferService := newTestService(t)
	createWallets(t, ctx, repository, map[string]int64{
		"wallet_1": 500,
		"wallet_2": 25,
	})

	_, err := transferService.CreateTransfer(ctx, domain.TransferCommand{
		IdempotencyKey: "key-conflict",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}

	_, err = transferService.CreateTransfer(ctx, domain.TransferCommand{
		IdempotencyKey: "key-conflict",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         101,
	})
	if !errors.Is(err, service.ErrIdempotencyConflict) {
		t.Fatalf("error = %v, want ErrIdempotencyConflict", err)
	}
	assertBalance(t, ctx, repository, "wallet_1", 400)
	assertBalance(t, ctx, repository, "wallet_2", 125)
}

func TestCreateTransferReturnsInFlightForIncompleteIdempotencyRecord(t *testing.T) {
	transferService := service.New(incompleteIdempotencyRepo{})

	_, err := transferService.CreateTransfer(context.Background(), domain.TransferCommand{
		IdempotencyKey: "key-in-flight",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})

	if !errors.Is(err, service.ErrIdempotencyInFlight) {
		t.Fatalf("error = %v, want ErrIdempotencyInFlight", err)
	}
}

func TestCreateTransferRecordsFailedTransferForInsufficientFunds(t *testing.T) {
	ctx := context.Background()
	repository, transferService := newTestService(t)
	createWallets(t, ctx, repository, map[string]int64{
		"wallet_1": 50,
		"wallet_2": 25,
	})
	command := domain.TransferCommand{
		IdempotencyKey: "key-insufficient",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	first, err := transferService.CreateTransfer(ctx, command)
	if err != nil {
		t.Fatalf("first CreateTransfer returned error: %v", err)
	}
	second, err := transferService.CreateTransfer(ctx, command)
	if err != nil {
		t.Fatalf("second CreateTransfer returned error: %v", err)
	}

	firstResponse := requireTransferResponse(t, first)
	secondResponse := requireTransferResponse(t, second)
	if first.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("status code = %d, want %d", first.StatusCode, http.StatusUnprocessableEntity)
	}
	if firstResponse.Transfer.State != domain.TransferFailed {
		t.Fatalf("transfer state = %s, want %s", firstResponse.Transfer.State, domain.TransferFailed)
	}
	if secondResponse.Transfer.ID != firstResponse.Transfer.ID {
		t.Fatalf("duplicate transfer ID = %q, want %q", secondResponse.Transfer.ID, firstResponse.Transfer.ID)
	}
	assertLedgerCount(t, ctx, repository, firstResponse.Transfer.ID, 0)
	assertBalance(t, ctx, repository, "wallet_1", 50)
	assertBalance(t, ctx, repository, "wallet_2", 25)
}

func TestCreateTransferPreventsDoubleSpendUnderConcurrentDebits(t *testing.T) {
	ctx := context.Background()
	repository, transferService := newTestService(t)
	createWallets(t, ctx, repository, map[string]int64{
		"source": 100,
		"sink_a": 0,
		"sink_b": 0,
	})

	commands := []domain.TransferCommand{
		{IdempotencyKey: "concurrent-a", FromWalletID: "source", ToWalletID: "sink_a", Amount: 80},
		{IdempotencyKey: "concurrent-b", FromWalletID: "source", ToWalletID: "sink_b", Amount: 80},
	}
	var wg sync.WaitGroup
	results := make(chan domain.Outcome, len(commands))
	errs := make(chan error, len(commands))
	for _, command := range commands {
		wg.Add(1)
		go func(command domain.TransferCommand) {
			defer wg.Done()
			outcome, err := transferService.CreateTransfer(ctx, command)
			if err != nil {
				errs <- err
				return
			}
			results <- outcome
		}(command)
	}
	wg.Wait()
	close(results)
	close(errs)

	for err := range errs {
		t.Fatalf("concurrent CreateTransfer returned error: %v", err)
	}

	var processed, failed int
	for outcome := range results {
		switch outcome.StatusCode {
		case http.StatusCreated:
			processed++
		case http.StatusUnprocessableEntity:
			failed++
		default:
			t.Fatalf("unexpected status code: %d", outcome.StatusCode)
		}
	}
	if processed != 1 || failed != 1 {
		t.Fatalf("processed=%d failed=%d, want processed=1 failed=1", processed, failed)
	}
	assertBalance(t, ctx, repository, "source", 20)

	sinkABalance := balance(t, ctx, repository, "sink_a")
	sinkBBalance := balance(t, ctx, repository, "sink_b")
	if sinkABalance+sinkBBalance != 80 {
		t.Fatalf("credited total = %d, want 80", sinkABalance+sinkBBalance)
	}
}

func newTestService(t *testing.T) (*store.SQLiteRepository, *service.Service) {
	t.Helper()
	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "wallets.db")
	repository, err := store.Open(ctx, fmt.Sprintf("file:%s?_foreign_keys=on&_busy_timeout=5000", dbPath))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	t.Cleanup(func() {
		if err := repository.Close(); err != nil {
			t.Fatalf("close repository: %v", err)
		}
	})
	return repository, service.New(repository)
}

func createWallets(t *testing.T, ctx context.Context, repository *store.SQLiteRepository, balances map[string]int64) {
	t.Helper()
	for id, initialBalance := range balances {
		if err := repository.CreateWallet(ctx, id, initialBalance); err != nil {
			t.Fatalf("create wallet %q: %v", id, err)
		}
	}
}

func requireTransferResponse(t *testing.T, outcome domain.Outcome) *domain.TransferResponse {
	t.Helper()
	if outcome.Body.Data == nil {
		t.Fatalf("outcome missing data: %+v", outcome.Body)
	}
	return outcome.Body.Data
}

func assertBalancedLedger(t *testing.T, entries []domain.LedgerEntry, amount int64) {
	t.Helper()
	var debit, credit int64
	for _, entry := range entries {
		switch entry.Type {
		case domain.LedgerDebit:
			debit += entry.Amount
		case domain.LedgerCredit:
			credit += entry.Amount
		default:
			t.Fatalf("unexpected ledger entry type %q", entry.Type)
		}
	}
	if debit != amount || credit != amount {
		t.Fatalf("ledger debit=%d credit=%d, want %d/%d", debit, credit, amount, amount)
	}
}

func assertBalance(t *testing.T, ctx context.Context, repository *store.SQLiteRepository, walletID string, expected int64) {
	t.Helper()
	actual := balance(t, ctx, repository, walletID)
	if actual != expected {
		t.Fatalf("wallet %q balance = %d, want %d", walletID, actual, expected)
	}
}

func balance(t *testing.T, ctx context.Context, repository *store.SQLiteRepository, walletID string) int64 {
	t.Helper()
	balance, found, err := repository.WalletBalance(ctx, walletID)
	if err != nil {
		t.Fatalf("read wallet %q balance: %v", walletID, err)
	}
	if !found {
		t.Fatalf("wallet %q not found", walletID)
	}
	return balance
}

func assertLedgerCount(t *testing.T, ctx context.Context, repository *store.SQLiteRepository, transferID string, expected int) {
	t.Helper()
	entries, err := repository.LedgerEntries(ctx, transferID)
	if err != nil {
		t.Fatalf("read ledger entries for transfer %q: %v", transferID, err)
	}
	if len(entries) != expected {
		t.Fatalf("ledger entry count = %d, want %d", len(entries), expected)
	}
}

type incompleteIdempotencyRepo struct{}

func (incompleteIdempotencyRepo) WithinTx(ctx context.Context, fn func(context.Context, service.TransferTx) error) error {
	return fn(ctx, incompleteIdempotencyTx{})
}

type incompleteIdempotencyTx struct{}

func (incompleteIdempotencyTx) FindOrCreateIdempotency(_ context.Context, key string, requestHash string) (service.IdempotencyRecord, bool, error) {
	return service.IdempotencyRecord{
		Key:         key,
		RequestHash: requestHash,
		Completed:   false,
	}, false, nil
}

func (incompleteIdempotencyTx) CompleteIdempotency(context.Context, string, domain.Outcome) error {
	panic("CompleteIdempotency should not be called for in-flight idempotency records")
}

func (incompleteIdempotencyTx) GetWallet(context.Context, string) (service.Wallet, bool, error) {
	panic("GetWallet should not be called for in-flight idempotency records")
}

func (incompleteIdempotencyTx) CreateTransfer(context.Context, domain.Transfer) error {
	panic("CreateTransfer should not be called for in-flight idempotency records")
}

func (incompleteIdempotencyTx) SetTransferState(context.Context, string, domain.TransferState, string) error {
	panic("SetTransferState should not be called for in-flight idempotency records")
}

func (incompleteIdempotencyTx) DebitWallet(context.Context, string, int64) (int64, bool, error) {
	panic("DebitWallet should not be called for in-flight idempotency records")
}

func (incompleteIdempotencyTx) CreditWallet(context.Context, string, int64) (int64, error) {
	panic("CreditWallet should not be called for in-flight idempotency records")
}

func (incompleteIdempotencyTx) CreateLedgerEntries(context.Context, []domain.LedgerEntry) error {
	panic("CreateLedgerEntries should not be called for in-flight idempotency records")
}
