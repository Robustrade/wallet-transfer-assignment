package postgres

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/devkarrahul2610/wallet-transfer-assignment/internal/postgres"
	"github.com/devkarrahul2610/wallet-transfer-assignment/internal/transfer"
)

func TestRepositoryTransferIdempotencyLedgerAndFailures(t *testing.T) {
	db := integrationDB(t)
	prefix := createWallets(t, db, 2, 1000)
	sourceID, destinationID := prefix+"_wallet_0", prefix+"_wallet_1"
	keyPrefix := "it_" + prefix
	t.Cleanup(func() { cleanupFixtures(t, db, prefix, keyPrefix) })

	repository := NewRepository(db)
	request := transfer.CreateRequest{
		IdempotencyKey: keyPrefix + "_success",
		FromWalletID:   sourceID,
		ToWalletID:     destinationID,
		Amount:         125,
	}

	first, err := repository.Create(context.Background(), request, hashRequest(t, request))
	if err != nil {
		t.Fatal(err)
	}
	if first.StatusCode != 201 {
		t.Fatalf("first status = %d, want 201; body: %s", first.StatusCode, first.Body)
	}
	var result transfer.Result
	if err := json.Unmarshal(first.Body, &result); err != nil {
		t.Fatalf("decode successful result: %v", err)
	}
	if result.State != "PROCESSED" || result.FromBalance != 875 || result.ToBalance != 1125 {
		t.Fatalf("unexpected transfer result: %+v", result)
	}

	replayed, err := repository.Create(context.Background(), request, hashRequest(t, request))
	if err != nil {
		t.Fatal(err)
	}
	if replayed.StatusCode != first.StatusCode || string(replayed.Body) != string(first.Body) {
		t.Fatalf("replay changed result: first=%+v replay=%+v", first, replayed)
	}

	changedRequest := request
	changedRequest.Amount = 126
	_, err = repository.Create(context.Background(), changedRequest, hashRequest(t, changedRequest))
	var apiError *transfer.Error
	if !errors.As(err, &apiError) || apiError.StatusCode != 409 {
		t.Fatalf("reusing key with different input returned %v, want idempotency conflict", err)
	}

	if got := scalarInt64(t, db, `SELECT count(*) FROM ledger_entries le JOIN transfers tr ON tr.id = le.transfer_id WHERE tr.from_wallet_id = $1`, sourceID); got != 2 {
		t.Fatalf("ledger entries for source wallet = %d, want 2", got)
	}
	if got := scalarInt64(t, db, `SELECT balance FROM wallets WHERE id = $1`, sourceID); got != 875 {
		t.Fatalf("source balance = %d, want 875", got)
	}
	if got := scalarInt64(t, db, `SELECT balance FROM wallets WHERE id = $1`, destinationID); got != 1125 {
		t.Fatalf("destination balance = %d, want 1125", got)
	}

	failedRequest := request
	failedRequest.IdempotencyKey = keyPrefix + "_insufficient"
	failedRequest.Amount = 1000
	failed, err := repository.Create(context.Background(), failedRequest, hashRequest(t, failedRequest))
	if err != nil {
		t.Fatal(err)
	}
	if failed.StatusCode != 409 {
		t.Fatalf("insufficient funds status = %d, want 409", failed.StatusCode)
	}
	failedReplay, err := repository.Create(context.Background(), failedRequest, hashRequest(t, failedRequest))
	if err != nil {
		t.Fatal(err)
	}
	if failedReplay.StatusCode != failed.StatusCode || string(failedReplay.Body) != string(failed.Body) {
		t.Fatalf("failed request replay changed result: first=%+v replay=%+v", failed, failedReplay)
	}
	if got := scalarInt64(t, db, `
		SELECT count(*) FROM ledger_entries le
		JOIN transfers tr ON tr.id = le.transfer_id
		WHERE tr.from_wallet_id = $1 AND tr.state = 'FAILED'`, sourceID); got != 0 {
		t.Fatalf("failed transfer ledger entry count = %d, want 0", got)
	}

	missingRequest := request
	missingRequest.IdempotencyKey = keyPrefix + "_missing"
	missingRequest.ToWalletID = prefix + "_not_provisioned"
	missing, err := repository.Create(context.Background(), missingRequest, hashRequest(t, missingRequest))
	if err != nil {
		t.Fatal(err)
	}
	if missing.StatusCode != 404 {
		t.Fatalf("unknown wallet status = %d, want 404", missing.StatusCode)
	}
	missingReplay, err := repository.Create(context.Background(), missingRequest, hashRequest(t, missingRequest))
	if err != nil {
		t.Fatal(err)
	}
	if missingReplay.StatusCode != missing.StatusCode || string(missingReplay.Body) != string(missing.Body) {
		t.Fatalf("unknown wallet replay changed result: first=%+v replay=%+v", missing, missingReplay)
	}
}

func TestRepositoryConcurrentDebitsDoNotOverdraw(t *testing.T) {
	db := integrationDB(t)
	prefix := createWallets(t, db, 3, 100)
	keyPrefix := "it_" + prefix
	t.Cleanup(func() { cleanupFixtures(t, db, prefix, keyPrefix) })

	repository := NewRepository(db)
	requests := []transfer.CreateRequest{
		{IdempotencyKey: keyPrefix + "_a", FromWalletID: prefix + "_wallet_0", ToWalletID: prefix + "_wallet_1", Amount: 75},
		{IdempotencyKey: keyPrefix + "_b", FromWalletID: prefix + "_wallet_0", ToWalletID: prefix + "_wallet_2", Amount: 75},
	}
	hashes := [][32]byte{hashRequest(t, requests[0]), hashRequest(t, requests[1])}

	var waitGroup sync.WaitGroup
	statuses := make(chan int, len(requests))
	errorsFound := make(chan error, len(requests))
	for index, request := range requests {
		request, requestHash := request, hashes[index]
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			outcome, err := repository.Create(context.Background(), request, requestHash)
			if err != nil {
				errorsFound <- err
				return
			}
			statuses <- outcome.StatusCode
		}()
	}
	waitGroup.Wait()
	close(statuses)
	close(errorsFound)
	for err := range errorsFound {
		t.Errorf("concurrent transfer failed: %v", err)
	}

	var succeeded, rejected int
	for status := range statuses {
		switch status {
		case 201:
			succeeded++
		case 409:
			rejected++
		default:
			t.Errorf("unexpected status %d", status)
		}
	}
	if succeeded != 1 || rejected != 1 {
		t.Fatalf("got %d successful and %d rejected transfers, want one of each", succeeded, rejected)
	}
	if got := scalarInt64(t, db, `SELECT balance FROM wallets WHERE id = $1`, prefix+"_wallet_0"); got != 25 {
		t.Fatalf("source balance = %d, want 25", got)
	}
	if got := scalarInt64(t, db, `SELECT count(*) FROM ledger_entries le JOIN transfers tr ON tr.id = le.transfer_id WHERE tr.from_wallet_id = $1`, prefix+"_wallet_0"); got != 2 {
		t.Fatalf("ledger entry count = %d, want 2", got)
	}
}

func integrationDB(t *testing.T) *sql.DB {
	t.Helper()
	connectionString := os.Getenv("TEST_DATABASE_URL")
	if connectionString == "" {
		t.Skip("set TEST_DATABASE_URL to a migrated PostgreSQL test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := postgres.Open(ctx, connectionString)
	if err != nil {
		t.Fatalf("connect to test database: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func createWallets(t *testing.T, db *sql.DB, count int, initialBalance int64) string {
	t.Helper()
	id, err := newUUID()
	if err != nil {
		t.Fatal(err)
	}
	prefix := "it_" + id
	for i := 0; i < count; i++ {
		walletID := fmt.Sprintf("%s_wallet_%d", prefix, i)
		if _, err := db.Exec(`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, walletID, initialBalance); err != nil {
			t.Fatalf("insert test wallet: %v", err)
		}
	}
	return prefix
}

func cleanupFixtures(t *testing.T, db *sql.DB, prefix, keyPrefix string) {
	t.Helper()
	if _, err := db.Exec(`
		DELETE FROM idempotency_records
		WHERE left(idempotency_key, char_length($1)) = $1`, keyPrefix); err != nil {
		t.Errorf("delete test idempotency records: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM ledger_entries WHERE transfer_id IN (
		SELECT id FROM transfers
		WHERE left(from_wallet_id, char_length($1)) = $1
		   OR left(to_wallet_id, char_length($1)) = $1
	)`, prefix); err != nil {
		t.Errorf("delete test ledger entries: %v", err)
	}
	if _, err := db.Exec(`
		DELETE FROM transfers
		WHERE left(from_wallet_id, char_length($1)) = $1
		   OR left(to_wallet_id, char_length($1)) = $1`, prefix); err != nil {
		t.Errorf("delete test transfers: %v", err)
	}
	if _, err := db.Exec(`DELETE FROM wallets WHERE left(id, char_length($1)) = $1`, prefix); err != nil {
		t.Errorf("delete test wallets: %v", err)
	}
}

func hashRequest(t *testing.T, request transfer.CreateRequest) [32]byte {
	t.Helper()
	canonicalRequest, err := json.Marshal(struct {
		FromWalletID string `json:"fromWalletId"`
		ToWalletID   string `json:"toWalletId"`
		Amount       int64  `json:"amount"`
	}{request.FromWalletID, request.ToWalletID, request.Amount})
	if err != nil {
		t.Fatal(err)
	}
	return sha256.Sum256(canonicalRequest)
}

func scalarInt64(t *testing.T, db *sql.DB, query string, argument any) int64 {
	t.Helper()
	var result int64
	if err := db.QueryRow(query, argument).Scan(&result); err != nil {
		t.Fatal(err)
	}
	return result
}
