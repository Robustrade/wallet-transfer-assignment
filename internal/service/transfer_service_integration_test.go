//go:build integration

// Run with: go test -tags=integration ./internal/service/... -run Integration -v
//
// Requires a reachable PostgreSQL instance with the schema from
// migrations/001_init.sql already applied. Configure via TEST_DATABASE_URL,
// e.g.:
//
//	export TEST_DATABASE_URL='postgres://postgres:postgres@localhost:5432/wallettransfer_test?sslmode=disable'
//
// These tests exercise the real postgres repositories (real row locks, real
// INSERT ... ON CONFLICT idempotency semantics) rather than fakes, because
// the properties under test — no double spend under concurrency, exactly-
// once under duplicate delivery — are precisely the properties that fakes
// tend to accidentally assume away.
package service_test

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"

	_ "github.com/lib/pq"

	"wallettransfer/internal/domain"
	"wallettransfer/internal/repository/postgres"
	"wallettransfer/internal/service"

	"github.com/joho/godotenv"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	_ := godotenv.Load("../../.env")
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	if err := db.Ping(); err != nil {
		t.Fatalf("ping db: %v", err)
	}
	// Isolate each test run: wipe tables. Order matters due to FKs.
	for _, tbl := range []string{"ledger_entries", "idempotency_records", "transfers", "wallets"} {
		if _, err := db.Exec("DELETE FROM " + tbl); err != nil {
			t.Fatalf("truncate %s: %v", tbl, err)
		}
	}
	return db
}

func newTestService(db *sql.DB) *service.TransferService {
	wallets := postgres.NewWalletRepo(db)
	transfers := postgres.NewTransferRepo(db)
	ledger := postgres.NewLedgerRepo(db)
	idem := postgres.NewIdempotencyRepo(db)
	return service.NewTransferService(db, wallets, transfers, ledger, idem, nil)
}

func mustCreateWallet(t *testing.T, db *sql.DB, id string, balance int64) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO wallets (id, balance) VALUES ($1, $2)`, id, balance); err != nil {
		t.Fatalf("create wallet %s: %v", id, err)
	}
}

func getBalance(t *testing.T, db *sql.DB, id string) int64 {
	t.Helper()
	var bal int64
	if err := db.QueryRow(`SELECT balance FROM wallets WHERE id = $1`, id).Scan(&bal); err != nil {
		t.Fatalf("get balance %s: %v", id, err)
	}
	return bal
}

func TestIntegration_SuccessfulTransfer_UpdatesBalancesAndLedger(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "alice", 1000)
	mustCreateWallet(t, db, "bob", 500)

	result, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "tx-1", FromWalletID: "alice", ToWalletID: "bob", Amount: 300,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if result.Transfer.Status != domain.TransferProcessed {
		t.Fatalf("expected PROCESSED, got %s", result.Transfer.Status)
	}

	if got := getBalance(t, db, "alice"); got != 700 {
		t.Errorf("alice balance = %d, want 700", got)
	}
	if got := getBalance(t, db, "bob"); got != 800 {
		t.Errorf("bob balance = %d, want 800", got)
	}

	rows, err := db.Query(`SELECT wallet_id, type, amount FROM ledger_entries WHERE transfer_id = $1 ORDER BY type`, result.Transfer.ID)
	if err != nil {
		t.Fatalf("query ledger: %v", err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var walletID, typ string
		var amount int64
		if err := rows.Scan(&walletID, &typ, &amount); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if amount != 300 {
			t.Errorf("ledger entry amount = %d, want 300", amount)
		}
	}
	if count != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", count)
	}
}

func TestIntegration_DuplicateIdempotencyKey_DoesNotDoubleTransfer(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "alice", 1000)
	mustCreateWallet(t, db, "bob", 0)

	req := domain.NewTransferRequest{IdempotencyKey: "dup-key", FromWalletID: "alice", ToWalletID: "bob", Amount: 250}

	first, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if first.Replayed {
		t.Fatalf("first call should not be a replay")
	}

	// Fire the same request 5 more times sequentially (simulating client retries).
	for i := 0; i < 5; i++ {
		second, err := svc.CreateTransfer(context.Background(), req)
		if err != nil {
			t.Fatalf("retry %d: %v", i, err)
		}
		if !second.Replayed {
			t.Fatalf("retry %d should be flagged as replayed", i)
		}
		if second.Transfer.ID != first.Transfer.ID {
			t.Fatalf("retry %d got different transfer id: %s vs %s", i, second.Transfer.ID, first.Transfer.ID)
		}
	}

	if got := getBalance(t, db, "alice"); got != 750 {
		t.Errorf("alice balance = %d, want 750 (transfer must not repeat)", got)
	}
	if got := getBalance(t, db, "bob"); got != 250 {
		t.Errorf("bob balance = %d, want 250", got)
	}

	var transferCount int
	if err := db.QueryRow(`SELECT count(*) FROM transfers WHERE idempotency_key = 'dup-key'`).Scan(&transferCount); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if transferCount != 1 {
		t.Fatalf("expected exactly 1 transfer row, got %d", transferCount)
	}
}

func TestIntegration_SameKeyDifferentPayload_IsConflict(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "alice", 1000)
	mustCreateWallet(t, db, "bob", 0)
	mustCreateWallet(t, db, "carol", 0)

	_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "reused-key", FromWalletID: "alice", ToWalletID: "bob", Amount: 100,
	})
	if err != nil {
		t.Fatalf("first call: %v", err)
	}

	_, err = svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "reused-key", FromWalletID: "alice", ToWalletID: "carol", Amount: 999,
	})
	if err == nil {
		t.Fatal("expected conflict error for reused key with different payload")
	}
	if !errors.Is(err, domain.ErrIdempotencyConflict) {
		t.Fatalf("expected ErrIdempotencyConflict, got %v", err)
	}
}

func TestIntegration_InsufficientFunds_FailsAndDoesNotMutateBalances(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "alice", 50)
	mustCreateWallet(t, db, "bob", 0)

	_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "too-much", FromWalletID: "alice", ToWalletID: "bob", Amount: 1000,
	})
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected ErrInsufficientFunds, got %v", err)
	}

	if got := getBalance(t, db, "alice"); got != 50 {
		t.Errorf("alice balance changed to %d, should remain 50", got)
	}
	if got := getBalance(t, db, "bob"); got != 0 {
		t.Errorf("bob balance changed to %d, should remain 0", got)
	}

	var status, reason string
	if err := db.QueryRow(`SELECT status, failure_reason FROM transfers WHERE idempotency_key = 'too-much'`).Scan(&status, &reason); err != nil {
		t.Fatalf("query transfer: %v", err)
	}
	if status != string(domain.TransferFailed) {
		t.Errorf("transfer status = %s, want FAILED", status)
	}

	// Retrying the exact same failing request should replay FAILED, not
	// attempt (and fail) again from scratch.
	_, err = svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "too-much", FromWalletID: "alice", ToWalletID: "bob", Amount: 1000,
	})
	if !errors.Is(err, domain.ErrInsufficientFunds) {
		t.Fatalf("expected replayed ErrInsufficientFunds, got %v", err)
	}
}

func TestIntegration_UnknownSourceWallet_FailsWithoutTransferRow(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "bob", 0)

	_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "missing-from", FromWalletID: "ghost", ToWalletID: "bob", Amount: 10,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
	assertNoTransferRow(t, db, "missing-from")
	assertIdempotencyCompletedWithoutTransfer(t, db, "missing-from")
}

func TestIntegration_UnknownDestinationWallet_FailsWithoutTransferRow(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "alice", 1000)

	_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "missing-to", FromWalletID: "alice", ToWalletID: "ghost", Amount: 10,
	})
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("expected ErrWalletNotFound, got %v", err)
	}
	if got := getBalance(t, db, "alice"); got != 1000 {
		t.Errorf("alice balance = %d, want 1000 (unknown dest must not debit)", got)
	}
	assertNoTransferRow(t, db, "missing-to")
	assertIdempotencyCompletedWithoutTransfer(t, db, "missing-to")
}

func TestIntegration_UnknownWallet_ReplayReturnsSameError(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "bob", 0)

	req := domain.NewTransferRequest{
		IdempotencyKey: "missing-from-retry", FromWalletID: "ghost", ToWalletID: "bob", Amount: 10,
	}
	_, err := svc.CreateTransfer(context.Background(), req)
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("first call: expected ErrWalletNotFound, got %v", err)
	}

	_, err = svc.CreateTransfer(context.Background(), req)
	if !errors.Is(err, domain.ErrWalletNotFound) {
		t.Fatalf("replay: expected ErrWalletNotFound, got %v", err)
	}
	assertNoTransferRow(t, db, "missing-from-retry")
}

func TestIntegration_ExactBalanceDrain_LeavesSourceAtZero(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "alice", 100)
	mustCreateWallet(t, db, "bob", 0)

	result, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "drain", FromWalletID: "alice", ToWalletID: "bob", Amount: 100,
	})
	if err != nil {
		t.Fatalf("CreateTransfer: %v", err)
	}
	if result.Transfer.Status != domain.TransferProcessed {
		t.Fatalf("expected PROCESSED, got %s", result.Transfer.Status)
	}
	if got := getBalance(t, db, "alice"); got != 0 {
		t.Errorf("alice balance = %d, want 0", got)
	}
	if got := getBalance(t, db, "bob"); got != 100 {
		t.Errorf("bob balance = %d, want 100", got)
	}
}

func TestIntegration_InvalidRequest_RejectedBeforeMutation(t *testing.T) {
	db := testDB(t)
	svc := newTestService(db)
	mustCreateWallet(t, db, "alice", 1000)
	mustCreateWallet(t, db, "bob", 0)

	_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
		IdempotencyKey: "", FromWalletID: "alice", ToWalletID: "bob", Amount: 10,
	})
	if !errors.Is(err, domain.ErrIdempotencyKeyEmpty) {
		t.Fatalf("expected ErrIdempotencyKeyEmpty, got %v", err)
	}
	if got := getBalance(t, db, "alice"); got != 1000 {
		t.Errorf("alice balance = %d, want 1000", got)
	}
}

func assertNoTransferRow(t *testing.T, db *sql.DB, key string) {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM transfers WHERE idempotency_key = $1`, key).Scan(&n); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if n != 0 {
		t.Fatalf("expected 0 transfer rows for key %s, got %d", key, n)
	}
}

func assertIdempotencyCompletedWithoutTransfer(t *testing.T, db *sql.DB, key string) {
	t.Helper()
	var status string
	var transferID sql.NullString
	if err := db.QueryRow(`SELECT status, transfer_id FROM idempotency_records WHERE idempotency_key = $1`, key).Scan(&status, &transferID); err != nil {
		t.Fatalf("query idempotency record: %v", err)
	}
	if status != string(domain.IdempotencyCompleted) {
		t.Errorf("idempotency status = %s, want COMPLETED", status)
	}
	if transferID.Valid {
		t.Errorf("transfer_id = %s, want NULL (no transfers row for unknown wallet)", transferID.String)
	}
}

// TestIntegration_ConcurrentTransfers_NoDoubleSpend is the key concurrency
// test: it fires many concurrent transfers out of a wallet with just enough
// balance for a limited number of them to succeed, and asserts that the
// final balance is exactly what it should be — no lost updates, no
// double-spend, and no negative balance — proving the row-lock + fixed
// lock-ordering strategy actually serializes conflicting transfers.
func TestIntegration_ConcurrentTransfers_NoDoubleSpend(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(25) // must exceed goroutine count or transactions will queue for connections, not fail
	svc := newTestService(db)

	mustCreateWallet(t, db, "shared", 1000)
	for i := 0; i < 20; i++ {
		mustCreateWallet(t, db, fmt.Sprintf("dest-%d", i), 0)
	}

	const n = 20
	const amount = 100 // 20 * 100 = 2000 requested against a balance of 1000: exactly half should succeed

	var wg sync.WaitGroup
	results := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", i),
				FromWalletID:   "shared",
				ToWalletID:     fmt.Sprintf("dest-%d", i),
				Amount:         amount,
			})
			results[i] = err
		}(i)
	}
	wg.Wait()

	succeeded, insufficientFunds := 0, 0
	for _, err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, domain.ErrInsufficientFunds):
			insufficientFunds++
		default:
			t.Errorf("unexpected error: %v", err)
		}
	}

	if succeeded != 10 {
		t.Errorf("expected exactly 10 transfers to succeed, got %d", succeeded)
	}
	if insufficientFunds != 10 {
		t.Errorf("expected exactly 10 transfers to fail with insufficient funds, got %d", insufficientFunds)
	}

	finalBalance := getBalance(t, db, "shared")
	if finalBalance != 0 {
		t.Errorf("shared wallet final balance = %d, want exactly 0 (no double spend, no lost updates)", finalBalance)
	}
	if finalBalance < 0 {
		t.Fatalf("balance went negative: %d — double spend occurred", finalBalance)
	}

	// Cross-check: sum of all destination balances must equal what left
	// the shared wallet.
	var totalReceived int64
	if err := db.QueryRow(`SELECT COALESCE(SUM(balance), 0) FROM wallets WHERE id LIKE 'dest-%'`).Scan(&totalReceived); err != nil {
		t.Fatalf("sum destinations: %v", err)
	}
	if totalReceived != 1000 {
		t.Errorf("total received across destination wallets = %d, want 1000", totalReceived)
	}
}

// TestIntegration_ConcurrentBidirectionalTransfers_BalancesUnchanged fires
// 20 A→B transfers and 20 B→A transfers of the same amount at the same
// time. Fixed lock ordering must prevent deadlock, every transfer must
// succeed, and each wallet's balance must end where it started because
// the money just moves back and forth.
func TestIntegration_ConcurrentBidirectionalTransfers_BalancesUnchanged(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(50)
	svc := newTestService(db)

	const (
		walletA  = "wallet-a"
		walletB  = "wallet-b"
		initialA = int64(5000)
		initialB = int64(5000)
		n        = 20
		amount   = int64(100)
	)

	mustCreateWallet(t, db, walletA, initialA)
	mustCreateWallet(t, db, walletB, initialB)

	var wg sync.WaitGroup
	results := make([]error, n*2)
	for i := 0; i < n; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
				IdempotencyKey: fmt.Sprintf("a-to-b-%d", i),
				FromWalletID:   walletA,
				ToWalletID:     walletB,
				Amount:         amount,
			})
			results[i] = err
		}(i)
		go func(i int) {
			defer wg.Done()
			_, err := svc.CreateTransfer(context.Background(), domain.NewTransferRequest{
				IdempotencyKey: fmt.Sprintf("b-to-a-%d", i),
				FromWalletID:   walletB,
				ToWalletID:     walletA,
				Amount:         amount,
			})
			results[n+i] = err
		}(i)
	}
	wg.Wait()

	for i, err := range results {
		if err != nil {
			t.Errorf("transfer %d failed: %v", i, err)
		}
	}

	if got := getBalance(t, db, walletA); got != initialA {
		t.Errorf("wallet-a balance = %d, want %d (net of equal A→B and B→A transfers must be zero)", got, initialA)
	}
	if got := getBalance(t, db, walletB); got != initialB {
		t.Errorf("wallet-b balance = %d, want %d (net of equal A→B and B→A transfers must be zero)", got, initialB)
	}

	var transferCount int
	if err := db.QueryRow(`SELECT count(*) FROM transfers WHERE status = $1`, domain.TransferProcessed).Scan(&transferCount); err != nil {
		t.Fatalf("count transfers: %v", err)
	}
	if transferCount != n*2 {
		t.Errorf("expected %d processed transfers, got %d", n*2, transferCount)
	}
}

// TestIntegration_ConcurrentDuplicateRequests_ExactlyOnce fires the SAME
// idempotency key from many goroutines simultaneously and asserts the
// transfer is applied exactly once, proving the idempotency claim mechanism
// is safe under true concurrent duplicate delivery, not just sequential
// retries.
func TestIntegration_ConcurrentDuplicateRequests_ExactlyOnce(t *testing.T) {
	db := testDB(t)
	db.SetMaxOpenConns(25)
	svc := newTestService(db)

	mustCreateWallet(t, db, "alice", 1000)
	mustCreateWallet(t, db, "bob", 0)

	const n = 15
	req := domain.NewTransferRequest{IdempotencyKey: "race-key", FromWalletID: "alice", ToWalletID: "bob", Amount: 100}

	var wg sync.WaitGroup
	transferIDs := make([]string, n)
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := svc.CreateTransfer(context.Background(), req)
			errs[i] = err
			if err == nil {
				transferIDs[i] = res.Transfer.ID
			}
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d got error: %v", i, err)
		}
	}
	first := transferIDs[0]
	for i, id := range transferIDs {
		if id != first {
			t.Errorf("goroutine %d got transfer id %s, want %s (all concurrent duplicates must resolve to the same transfer)", i, id, first)
		}
	}

	if got := getBalance(t, db, "alice"); got != 900 {
		t.Errorf("alice balance = %d, want 900 (transfer applied exactly once)", got)
	}
	if got := getBalance(t, db, "bob"); got != 100 {
		t.Errorf("bob balance = %d, want 100", got)
	}
}
