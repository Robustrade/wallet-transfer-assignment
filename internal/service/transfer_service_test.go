package service_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"testing"

	"github.com/rohitkumar27/wallet-transfer-assignment/internal/domain"
	"github.com/rohitkumar27/wallet-transfer-assignment/internal/service"
)

func newTestService(t *testing.T) (*service.TransferService, *sql.DB) {
	t.Helper()
	db := testDB(t)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return service.NewTransferService(db, logger), db
}

func walletBalance(t *testing.T, db *sql.DB, walletID string) int64 {
	t.Helper()
	var balance int64
	if err := db.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, walletID).Scan(&balance); err != nil {
		t.Fatalf("read balance for %s: %v", walletID, err)
	}
	return balance
}

func ledgerEntryCount(t *testing.T, db *sql.DB, transferID string) int {
	t.Helper()
	var count int
	if err := db.QueryRow(`SELECT count(*) FROM ledger_entries WHERE transfer_id = $1`, transferID).Scan(&count); err != nil {
		t.Fatalf("count ledger entries: %v", err)
	}
	return count
}

func decodeTransfer(t *testing.T, body []byte) service.TransferResponse {
	t.Helper()
	var resp service.TransferResponse
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("decode transfer response: %v (body=%s)", err, body)
	}
	return resp
}

func TestCreateTransfer_Success(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	fromBefore := walletBalance(t, db, "wallet_1")
	toBefore := walletBalance(t, db, "wallet_2")

	result, err := svc.CreateTransfer(ctx, domain.TransferRequest{
		IdempotencyKey: "test-success-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1000,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 201 {
		t.Fatalf("expected 201, got %d (body=%s)", result.StatusCode, result.Body)
	}

	tr := decodeTransfer(t, result.Body)
	if tr.Status != string(domain.TransferProcessed) {
		t.Fatalf("expected PROCESSED, got %s", tr.Status)
	}

	if got := walletBalance(t, db, "wallet_1"); got != fromBefore-1000 {
		t.Fatalf("from wallet balance = %d, want %d", got, fromBefore-1000)
	}
	if got := walletBalance(t, db, "wallet_2"); got != toBefore+1000 {
		t.Fatalf("to wallet balance = %d, want %d", got, toBefore+1000)
	}
	if got := ledgerEntryCount(t, db, tr.ID); got != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", got)
	}
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	result, err := svc.CreateTransfer(ctx, domain.TransferRequest{
		IdempotencyKey: "test-insufficient-1",
		FromWalletID:   "wallet_3", // seeded with 0 balance
		ToWalletID:     "wallet_2",
		Amount:         500,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 409 {
		t.Fatalf("expected 409, got %d (body=%s)", result.StatusCode, result.Body)
	}

	tr := decodeTransfer(t, result.Body)
	if tr.Status != string(domain.TransferFailed) {
		t.Fatalf("expected FAILED, got %s", tr.Status)
	}
	if got := ledgerEntryCount(t, db, tr.ID); got != 0 {
		t.Fatalf("expected no ledger entries for a failed transfer, got %d", got)
	}
	if got := walletBalance(t, db, "wallet_3"); got != 0 {
		t.Fatalf("wallet_3 balance should be unchanged, got %d", got)
	}
}

func TestCreateTransfer_UnknownWallet(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	result, err := svc.CreateTransfer(ctx, domain.TransferRequest{
		IdempotencyKey: "test-unknown-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "does_not_exist",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.StatusCode != 404 {
		t.Fatalf("expected 404, got %d (body=%s)", result.StatusCode, result.Body)
	}
}

func TestCreateTransfer_IdempotentReplay(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	req := domain.TransferRequest{
		IdempotencyKey: "test-replay-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         2000,
	}

	first, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}
	balanceAfterFirst := walletBalance(t, db, "wallet_1")

	second, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error on replay: %v", err)
	}

	if second.StatusCode != first.StatusCode {
		t.Fatalf("replay status code = %d, want %d", second.StatusCode, first.StatusCode)
	}
	if string(second.Body) != string(first.Body) {
		t.Fatalf("replay body = %s, want %s", second.Body, first.Body)
	}
	if got := walletBalance(t, db, "wallet_1"); got != balanceAfterFirst {
		t.Fatalf("replay must not move money again: balance = %d, want %d", got, balanceAfterFirst)
	}

	tr := decodeTransfer(t, first.Body)
	if got := ledgerEntryCount(t, db, tr.ID); got != 2 {
		t.Fatalf("replay must not create extra ledger entries, got %d", got)
	}
}

func TestCreateTransfer_SameKeyDifferentPayload(t *testing.T) {
	svc, _ := newTestService(t)
	ctx := context.Background()

	_, err := svc.CreateTransfer(ctx, domain.TransferRequest{
		IdempotencyKey: "test-conflict-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	})
	if err != nil {
		t.Fatalf("unexpected error on first call: %v", err)
	}

	_, err = svc.CreateTransfer(ctx, domain.TransferRequest{
		IdempotencyKey: "test-conflict-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         999, // different amount, same key
	})
	if err == nil {
		t.Fatal("expected an idempotency conflict error")
	}
	if !isIdempotencyConflict(err) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

// TestCreateTransfer_ConcurrentDebits fires many concurrent transfers out
// of a single wallet where the combined amount exceeds the balance, and
// asserts the database enforces correct accounting under real concurrency:
// no negative balance, and the final balance matches exactly the sum of
// the transfers that were actually marked PROCESSED. Run with `-race`.
func TestCreateTransfer_ConcurrentDebits(t *testing.T) {
	svc, db := newTestService(t)
	ctx := context.Background()

	const workers = 20
	const amount = int64(1000)
	startingBalance := walletBalance(t, db, "wallet_1")

	var wg sync.WaitGroup
	statuses := make([]string, workers)
	errs := make([]error, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			result, err := svc.CreateTransfer(ctx, domain.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         amount,
			})
			if err != nil {
				errs[i] = err
				return
			}
			statuses[i] = decodeTransfer(t, result.Body).Status
		}(i)
	}
	wg.Wait()

	var processed int64
	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d returned unexpected error: %v", i, err)
		}
		if statuses[i] == string(domain.TransferProcessed) {
			processed++
		}
	}

	finalBalance := walletBalance(t, db, "wallet_1")
	if finalBalance < 0 {
		t.Fatalf("balance went negative: %d", finalBalance)
	}
	want := startingBalance - processed*amount
	if finalBalance != want {
		t.Fatalf("final balance = %d, want %d (processed=%d)", finalBalance, want, processed)
	}
}

func isIdempotencyConflict(err error) bool {
	return errors.Is(err, domain.ErrIdempotencyConflict)
}
