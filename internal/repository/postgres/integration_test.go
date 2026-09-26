//go:build integration

package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"os"
	"sync"
	"testing"
	"time"

	"wallet-transfer-assignment/internal/domain"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/testcontainers/testcontainers-go"
	postgrescontainer "github.com/testcontainers/testcontainers-go/modules/postgres"
)

func integrationDatabase(t *testing.T) string {
	t.Helper()
	if databaseURL := os.Getenv("TEST_DATABASE_URL"); databaseURL != "" {
		return databaseURL
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgrescontainer.Run(ctx, "postgres:16-alpine",
		postgrescontainer.WithDatabase("wallet_test"),
		postgrescontainer.WithUsername("wallet"),
		postgrescontainer.WithPassword("wallet"),
	)
	if err != nil {
		t.Fatalf("start PostgreSQL Testcontainer: %v", err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate PostgreSQL Testcontainer: %v", err)
		}
	})
	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("get PostgreSQL connection string: %v", err)
	}
	return databaseURL
}

func openIntegrationStore(t *testing.T, databaseURL string, attempts int) *Store {
	t.Helper()
	store, err := Open(context.Background(), databaseURL, attempts)
	if err != nil {
		t.Fatalf("open PostgreSQL repository: %v", err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close PostgreSQL repository: %v", err)
		}
	})
	return store
}

func TestIntegrationConcurrentIdempotentTransferHasOneEffect(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 12)
	prefix := fmt.Sprintf("it_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := store.CreateWallet(ctx, prefix+"_from", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWallet(ctx, prefix+"_to", 0); err != nil {
		t.Fatal(err)
	}
	request := domain.TransferRequest{IdempotencyKey: prefix + "_key", FromWalletID: prefix + "_from", ToWalletID: prefix + "_to", Amount: 40}

	results := make([]domain.TransferResult, 2)
	callErrors := make([]error, 2)
	var group sync.WaitGroup
	for index := range results {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			results[index], callErrors[index] = store.ExecuteTransfer(ctx, request)
		}(index)
	}
	group.Wait()
	for _, err := range callErrors {
		if err != nil {
			t.Fatalf("concurrent idempotent transfer: %v", err)
		}
	}
	if results[0].Transfer.ID != results[1].Transfer.ID || results[0].Transfer.Status != domain.TransferProcessed || results[1].Transfer.Status != domain.TransferProcessed {
		t.Fatalf("requests did not replay one processed transfer: %+v %+v", results[0], results[1])
	}
	if len(results[0].Transfer.LedgerEntries) != 2 || len(results[1].Transfer.LedgerEntries) != 2 {
		t.Fatalf("saved response does not include double-entry ledger: %+v %+v", results[0], results[1])
	}
	wallet, err := store.GetWallet(ctx, request.FromWalletID)
	if err != nil || wallet.Balance != 60 {
		t.Fatalf("source balance=%d err=%v, want 60", wallet.Balance, err)
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var ledgerCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = $1`, results[0].Transfer.ID).Scan(&ledgerCount); err != nil {
		t.Fatal(err)
	}
	if ledgerCount != 2 {
		t.Fatalf("ledger has %d entries, want 2", ledgerCount)
	}
}

func TestIntegrationOppositeDirectionTransfersAvoidDeadlock(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 20)
	prefix := fmt.Sprintf("it_deadlock_%d", time.Now().UnixNano())
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	walletA, walletB := prefix+"_a", prefix+"_b"
	if _, err := store.CreateWallet(ctx, walletA, 100); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWallet(ctx, walletB, 100); err != nil {
		t.Fatal(err)
	}
	const transfersEachDirection = 10
	requests := make([]domain.TransferRequest, 0, transfersEachDirection*2)
	for index := 0; index < transfersEachDirection; index++ {
		requests = append(requests,
			domain.TransferRequest{IdempotencyKey: fmt.Sprintf("%s_ab_%d", prefix, index), FromWalletID: walletA, ToWalletID: walletB, Amount: 1},
			domain.TransferRequest{IdempotencyKey: fmt.Sprintf("%s_ba_%d", prefix, index), FromWalletID: walletB, ToWalletID: walletA, Amount: 1},
		)
	}
	results := make([]domain.TransferResult, len(requests))
	callErrors := make([]error, len(requests))
	var group sync.WaitGroup
	for index, request := range requests {
		group.Add(1)
		go func(index int, request domain.TransferRequest) {
			defer group.Done()
			results[index], callErrors[index] = store.ExecuteTransfer(ctx, request)
		}(index, request)
	}
	group.Wait()
	for index, err := range callErrors {
		if err != nil {
			t.Fatalf("opposite-direction transfer %d failed: %v", index, err)
		}
		if results[index].Transfer.Status != domain.TransferProcessed {
			t.Fatalf("opposite-direction transfer %d status=%s", index, results[index].Transfer.Status)
		}
	}
}

func TestIntegrationLedgerFailureRollsBackBalancesAndReleasesClaim(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 3)
	prefix := fmt.Sprintf("it_rollback_%d", time.Now().UnixNano())
	ctx := context.Background()
	fromID, toID := prefix+"_from", prefix+"_to"
	if _, err := store.CreateWallet(ctx, fromID, 20); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWallet(ctx, toID, 0); err != nil {
		t.Fatal(err)
	}
	constraint := prefix + "_reject_credit"
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE ledger_entries ADD CONSTRAINT %s CHECK (wallet_id <> '%s')", constraint, toID)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = store.db.ExecContext(context.Background(), fmt.Sprintf("ALTER TABLE ledger_entries DROP CONSTRAINT IF EXISTS %s", constraint))
	})
	request := domain.TransferRequest{IdempotencyKey: prefix + "_key", FromWalletID: fromID, ToWalletID: toID, Amount: 5}
	if _, err := store.ExecuteTransfer(ctx, request); err == nil {
		t.Fatal("expected injected ledger constraint failure")
	}
	wallet, err := store.GetWallet(ctx, fromID)
	if err != nil || wallet.Balance != 20 {
		t.Fatalf("source balance=%d err=%v, want unchanged 20", wallet.Balance, err)
	}
	if _, err := store.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE ledger_entries DROP CONSTRAINT %s", constraint)); err != nil {
		t.Fatal(err)
	}
	result, err := store.ExecuteTransfer(ctx, request)
	if err != nil || result.Transfer.Status != domain.TransferProcessed {
		t.Fatalf("retry after rollback result=%+v err=%v", result, err)
	}
}

func TestIntegrationConcurrentMigrationStartup(t *testing.T) {
	databaseURL := integrationDatabase(t)
	const workers = 4
	stores := make([]*Store, workers)
	callErrors := make([]error, workers)
	var group sync.WaitGroup
	for index := 0; index < workers; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			stores[index], callErrors[index] = Open(context.Background(), databaseURL, 3)
		}(index)
	}
	group.Wait()
	for index, err := range callErrors {
		if err != nil {
			t.Fatalf("concurrent migration startup %d: %v", index, err)
		}
	}
	for _, store := range stores {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	}
}

func TestIntegrationPayloadHashHandlesPipeCharacters(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 5)
	prefix := fmt.Sprintf("it_hash_%d", time.Now().UnixNano())
	ctx := context.Background()
	for _, walletID := range []string{prefix + "_a|b", prefix + "_c", prefix + "_a", prefix + "_b|c"} {
		if _, err := store.CreateWallet(ctx, walletID, 100); err != nil {
			t.Fatal(err)
		}
	}
	first := domain.TransferRequest{IdempotencyKey: prefix + "_key", FromWalletID: prefix + "_a|b", ToWalletID: prefix + "_c", Amount: 10}
	if _, err := store.ExecuteTransfer(ctx, first); err != nil {
		t.Fatal(err)
	}
	collision := domain.TransferRequest{IdempotencyKey: first.IdempotencyKey, FromWalletID: prefix + "_a", ToWalletID: prefix + "_b|c", Amount: 10}
	if _, err := store.ExecuteTransfer(ctx, collision); !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("pipe-containing payload should conflict, got %v", err)
	}
}

func TestIntegrationIncompleteClaimReturnsRetryableConflict(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 1)
	prefix := fmt.Sprintf("it_claim_%d", time.Now().UnixNano())
	request := domain.TransferRequest{IdempotencyKey: prefix + "_key", FromWalletID: prefix + "_from", ToWalletID: prefix + "_to", Amount: 1}
	if _, err := store.CreateWallet(context.Background(), request.FromWalletID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWallet(context.Background(), request.ToWalletID, 0); err != nil {
		t.Fatal(err)
	}
	hash, err := hashRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(context.Background(), `
INSERT INTO idempotency_records(key, request_hash, state, owner_token, lease_expires_at)
VALUES ($1, $2, $3, 'active-owner', NOW() + INTERVAL '1 minute')`, request.IdempotencyKey, hash[:], claimInProgress); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ExecuteTransfer(context.Background(), request); !errors.Is(err, domain.ErrIdempotencyInProgress) {
		t.Fatalf("incomplete claim error = %v, want ErrIdempotencyInProgress", err)
	}
}

func TestIntegrationExpiredClaimCanBeRecovered(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 3)
	prefix := fmt.Sprintf("it_expired_claim_%d", time.Now().UnixNano())
	ctx := context.Background()
	request := domain.TransferRequest{IdempotencyKey: prefix + "_key", FromWalletID: prefix + "_from", ToWalletID: prefix + "_to", Amount: 1}
	if _, err := store.CreateWallet(ctx, request.FromWalletID, 2); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWallet(ctx, request.ToWalletID, 0); err != nil {
		t.Fatal(err)
	}
	hash, err := hashRequest(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `
INSERT INTO idempotency_records(key, request_hash, state, owner_token, lease_expires_at)
VALUES ($1, $2, $3, 'abandoned-owner', NOW() - INTERVAL '1 second')`, request.IdempotencyKey, hash[:], claimInProgress); err != nil {
		t.Fatal(err)
	}
	newOwner, claimed, err := store.claimIdempotencyKey(ctx, request.IdempotencyKey, hash)
	if err != nil || !claimed {
		t.Fatalf("expired claim was not taken over: claimed=%v err=%v", claimed, err)
	}
	if _, err := store.processClaimedTransfer(ctx, request, "abandoned-owner"); !errors.Is(err, domain.ErrIdempotencyInProgress) {
		t.Fatalf("stale owner was not fenced: err=%v", err)
	}
	result, err := store.processClaimedTransfer(ctx, request, newOwner)
	if err != nil || result.Transfer.Status != domain.TransferProcessed {
		t.Fatalf("new claim owner could not process transfer: result=%+v err=%v", result, err)
	}
	wallet, err := store.GetWallet(ctx, request.FromWalletID)
	if err != nil || wallet.Balance != 1 {
		t.Fatalf("recovered transfer source balance=%d err=%v, want 1", wallet.Balance, err)
	}
}

func TestIntegrationOpeningBalanceCreatesBalancedLedgerEntries(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 3)
	prefix := fmt.Sprintf("it_opening_%d", time.Now().UnixNano())
	openingBalance := int64(75)
	if _, err := store.CreateWallet(context.Background(), prefix, openingBalance); err != nil {
		t.Fatal(err)
	}
	rows, err := store.db.QueryContext(context.Background(), `
SELECT account_id, entry_type, amount
FROM ledger_entries WHERE operation_id = $1 ORDER BY entry_type`, "opening:"+prefix)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	entries := make(map[string]struct {
		account string
		amount  int64
	})
	for rows.Next() {
		var account, entryType string
		var amount int64
		if err := rows.Scan(&account, &entryType, &amount); err != nil {
			t.Fatal(err)
		}
		entries[entryType] = struct {
			account string
			amount  int64
		}{account: account, amount: amount}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	debit := entries["DEBIT"]
	credit := entries["CREDIT"]
	if len(entries) != 2 || debit.account != "system:funding" || credit.account != prefix || debit.amount != openingBalance || credit.amount != openingBalance {
		t.Fatalf("opening ledger is not balanced: %+v", entries)
	}
}

func TestIntegrationDestinationOverflowDoesNotDebitSource(t *testing.T) {
	databaseURL := integrationDatabase(t)
	store := openIntegrationStore(t, databaseURL, 3)
	prefix := fmt.Sprintf("it_overflow_%d", time.Now().UnixNano())
	ctx := context.Background()
	if _, err := store.CreateWallet(ctx, prefix+"_from", 5); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreateWallet(ctx, prefix+"_to", math.MaxInt64); err != nil {
		t.Fatal(err)
	}
	result, err := store.ExecuteTransfer(ctx, domain.TransferRequest{
		IdempotencyKey: prefix + "_key", FromWalletID: prefix + "_from", ToWalletID: prefix + "_to", Amount: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.HTTPStatus != 422 || result.Transfer.Status != domain.TransferFailed || result.Transfer.FailureReason != "destination balance overflow" {
		t.Fatalf("unexpected overflow result: %+v", result)
	}
	wallet, err := store.GetWallet(ctx, prefix+"_from")
	if err != nil || wallet.Balance != 5 {
		t.Fatalf("source balance=%d err=%v, want unchanged balance 5", wallet.Balance, err)
	}
}
