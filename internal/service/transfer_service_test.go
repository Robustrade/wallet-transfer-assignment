package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"sync"
	"testing"

	"github.com/wallet-transfer-assignment/internal/database"
	"github.com/wallet-transfer-assignment/internal/domain"
	"github.com/wallet-transfer-assignment/internal/repository"
)

// testSetup creates a real SQLite in-memory database with migrations and seeded wallets.
// Returns the service, DB handle, and a cleanup function.
func testSetup(t *testing.T) (*TransferService, *sql.DB) {
	t.Helper()

	db, err := database.NewSQLiteDB(":memory:")
	if err != nil {
		t.Fatalf("failed to create test db: %v", err)
	}

	if err := database.RunSQLiteMigrations(db); err != nil {
		t.Fatalf("failed to run migrations: %v", err)
	}

	// Seed wallets
	wallets := []struct {
		id      string
		balance int64
	}{
		{"wallet_1", 10000},
		{"wallet_2", 10000},
		{"wallet_3", 5000},
	}
	for _, w := range wallets {
		_, err := db.Exec("INSERT INTO wallets (id, balance) VALUES (?, ?)", w.id, w.balance)
		if err != nil {
			t.Fatalf("failed to seed wallet %s: %v", w.id, err)
		}
	}

	repo := repository.NewSQLiteRepository(db)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	svc := NewTransferService(repo, logger)

	t.Cleanup(func() { db.Close() })

	return svc, db
}

func TestSuccessfulTransfer(t *testing.T) {
	svc, db := testSetup(t)
	ctx := context.Background()

	req := &domain.TransferRequest{
		IdempotencyKey: "test-success-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	result, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", result.StatusCode)
	}

	// Verify response body
	var resp domain.TransferResponse
	if err := json.Unmarshal(result.Body, &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}
	if resp.Status != domain.TransferStatusProcessed {
		t.Errorf("expected PROCESSED, got %s", resp.Status)
	}
	if resp.Amount != 100 {
		t.Errorf("expected amount 100, got %d", resp.Amount)
	}
	if resp.FromWalletID != "wallet_1" {
		t.Errorf("expected from wallet_1, got %s", resp.FromWalletID)
	}
	if resp.ToWalletID != "wallet_2" {
		t.Errorf("expected to wallet_2, got %s", resp.ToWalletID)
	}

	// Verify balances
	var fromBalance, toBalance int64
	db.QueryRow("SELECT balance FROM wallets WHERE id = ?", "wallet_1").Scan(&fromBalance)
	db.QueryRow("SELECT balance FROM wallets WHERE id = ?", "wallet_2").Scan(&toBalance)

	if fromBalance != 9900 {
		t.Errorf("expected wallet_1 balance 9900, got %d", fromBalance)
	}
	if toBalance != 10100 {
		t.Errorf("expected wallet_2 balance 10100, got %d", toBalance)
	}

	// Verify ledger entries
	var entryCount int
	db.QueryRow("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?", resp.ID).Scan(&entryCount)
	if entryCount != 2 {
		t.Errorf("expected 2 ledger entries, got %d", entryCount)
	}
}

func TestIdempotency_DuplicateRequest(t *testing.T) {
	svc, db := testSetup(t)
	ctx := context.Background()

	req := &domain.TransferRequest{
		IdempotencyKey: "test-idempotent-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         200,
	}

	// First request
	result1, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first request error: %v", err)
	}

	// Second request with same idempotency key
	result2, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("second request error: %v", err)
	}

	// Both should return the same response
	if result1.StatusCode != result2.StatusCode {
		t.Errorf("status codes differ: %d vs %d", result1.StatusCode, result2.StatusCode)
	}
	if string(result1.Body) != string(result2.Body) {
		t.Errorf("response bodies differ:\n  first:  %s\n  second: %s", result1.Body, result2.Body)
	}

	// Verify balance was only debited once
	var fromBalance int64
	db.QueryRow("SELECT balance FROM wallets WHERE id = ?", "wallet_1").Scan(&fromBalance)
	if fromBalance != 9800 {
		t.Errorf("expected wallet_1 balance 9800 (debited once), got %d", fromBalance)
	}

	// Verify only 1 transfer exists
	var transferCount int
	db.QueryRow("SELECT COUNT(*) FROM transfers WHERE idempotency_key = ?", "test-idempotent-1").Scan(&transferCount)
	if transferCount != 1 {
		t.Errorf("expected 1 transfer, got %d", transferCount)
	}

	// Verify only 2 ledger entries exist (not 4)
	var entryCount int
	db.QueryRow("SELECT COUNT(*) FROM ledger_entries").Scan(&entryCount)
	if entryCount != 2 {
		t.Errorf("expected 2 ledger entries total, got %d", entryCount)
	}
}

func TestInsufficientBalance(t *testing.T) {
	svc, db := testSetup(t)
	ctx := context.Background()

	req := &domain.TransferRequest{
		IdempotencyKey: "test-insufficient-1",
		FromWalletID:   "wallet_3", // balance = 5000
		ToWalletID:     "wallet_2",
		Amount:         99999, // way more than 5000
	}

	result, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("expected status 422, got %d", result.StatusCode)
	}

	// Verify response shows FAILED status
	var resp domain.TransferResponse
	json.Unmarshal(result.Body, &resp)
	if resp.Status != domain.TransferStatusFailed {
		t.Errorf("expected FAILED, got %s", resp.Status)
	}

	// Verify balance is unchanged
	var balance int64
	db.QueryRow("SELECT balance FROM wallets WHERE id = ?", "wallet_3").Scan(&balance)
	if balance != 5000 {
		t.Errorf("expected wallet_3 balance 5000 (unchanged), got %d", balance)
	}

	// Verify no ledger entries were created for the failed transfer
	var entryCount int
	db.QueryRow("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?", resp.ID).Scan(&entryCount)
	if entryCount != 0 {
		t.Errorf("expected 0 ledger entries for failed transfer, got %d", entryCount)
	}
}

func TestInvalidRequest_MissingFields(t *testing.T) {
	svc, _ := testSetup(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *domain.TransferRequest
	}{
		{
			name: "missing idempotency key",
			req: &domain.TransferRequest{
				FromWalletID: "wallet_1",
				ToWalletID:   "wallet_2",
				Amount:       100,
			},
		},
		{
			name: "missing from wallet",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-1",
				ToWalletID:     "wallet_2",
				Amount:         100,
			},
		},
		{
			name: "missing to wallet",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-2",
				FromWalletID:   "wallet_1",
				Amount:         100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateTransfer(ctx, tt.req)
			if err == nil {
				t.Error("expected error, got nil")
			}
			if err != domain.ErrInvalidRequest {
				t.Errorf("expected ErrInvalidRequest, got %v", err)
			}
		})
	}
}

func TestSameWalletTransfer(t *testing.T) {
	svc, _ := testSetup(t)
	ctx := context.Background()

	req := &domain.TransferRequest{
		IdempotencyKey: "test-same-wallet",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_1",
		Amount:         100,
	}

	_, err := svc.CreateTransfer(ctx, req)
	if err == nil {
		t.Error("expected error, got nil")
	}
	if err != domain.ErrSameWallet {
		t.Errorf("expected ErrSameWallet, got %v", err)
	}
}

func TestWalletNotFound(t *testing.T) {
	svc, _ := testSetup(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  *domain.TransferRequest
	}{
		{
			name: "from wallet not found",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-not-found-from",
				FromWalletID:   "nonexistent_wallet",
				ToWalletID:     "wallet_2",
				Amount:         100,
			},
		},
		{
			name: "to wallet not found",
			req: &domain.TransferRequest{
				IdempotencyKey: "test-not-found-to",
				FromWalletID:   "wallet_1",
				ToWalletID:     "nonexistent_wallet",
				Amount:         100,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := svc.CreateTransfer(ctx, tt.req)
			if err == nil {
				t.Error("expected error, got nil")
			}
			if err != domain.ErrWalletNotFound {
				t.Errorf("expected ErrWalletNotFound, got %v", err)
			}
		})
	}
}

func TestInvalidAmount(t *testing.T) {
	svc, _ := testSetup(t)
	ctx := context.Background()

	tests := []struct {
		name   string
		amount int64
	}{
		{"zero amount", 0},
		{"negative amount", -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &domain.TransferRequest{
				IdempotencyKey: "test-invalid-amount-" + tt.name,
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         tt.amount,
			}
			_, err := svc.CreateTransfer(ctx, req)
			if err != domain.ErrInvalidAmount {
				t.Errorf("expected ErrInvalidAmount, got %v", err)
			}
		})
	}
}

func TestLedgerConsistency(t *testing.T) {
	svc, db := testSetup(t)
	ctx := context.Background()

	// Perform multiple transfers
	transfers := []struct {
		key    string
		from   string
		to     string
		amount int64
	}{
		{"ledger-1", "wallet_1", "wallet_2", 100},
		{"ledger-2", "wallet_2", "wallet_3", 50},
		{"ledger-3", "wallet_3", "wallet_1", 25},
		{"ledger-4", "wallet_1", "wallet_3", 200},
	}

	for _, tr := range transfers {
		req := &domain.TransferRequest{
			IdempotencyKey: tr.key,
			FromWalletID:   tr.from,
			ToWalletID:     tr.to,
			Amount:         tr.amount,
		}
		_, err := svc.CreateTransfer(ctx, req)
		if err != nil {
			t.Fatalf("transfer %s failed: %v", tr.key, err)
		}
	}

	// Verify: sum of all DEBITs == sum of all CREDITs
	var totalDebits, totalCredits int64
	db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'DEBIT'").Scan(&totalDebits)
	db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE entry_type = 'CREDIT'").Scan(&totalCredits)

	if totalDebits != totalCredits {
		t.Errorf("ledger imbalance: total debits=%d, total credits=%d", totalDebits, totalCredits)
	}

	// Verify: total wallet balances are conserved (should still sum to 25000)
	var totalBalance int64
	db.QueryRow("SELECT COALESCE(SUM(balance), 0) FROM wallets").Scan(&totalBalance)
	if totalBalance != 25000 {
		t.Errorf("expected total balance 25000, got %d", totalBalance)
	}

	// Verify: each transfer has exactly 2 ledger entries
	var transferCount, entryCount int
	db.QueryRow("SELECT COUNT(*) FROM transfers WHERE status = 'PROCESSED'").Scan(&transferCount)
	db.QueryRow("SELECT COUNT(*) FROM ledger_entries").Scan(&entryCount)

	if entryCount != transferCount*2 {
		t.Errorf("expected %d ledger entries for %d transfers, got %d", transferCount*2, transferCount, entryCount)
	}
}

func TestConcurrentTransfers(t *testing.T) {
	svc, db := testSetup(t)
	ctx := context.Background()

	// wallet_1 has balance 10000
	// Launch 20 goroutines each trying to debit 1000 from wallet_1
	// Only 10 should succeed, the rest should fail with insufficient balance
	numGoroutines := 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	successCount := 0
	failCount := 0
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		go func(i int) {
			defer wg.Done()
			req := &domain.TransferRequest{
				IdempotencyKey: "concurrent-" + strconv.Itoa(i),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         1000,
			}

			result, err := svc.CreateTransfer(ctx, req)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				// Unexpected error
				t.Errorf("goroutine %d: unexpected error: %v", i, err)
				return
			}

			if result.StatusCode == http.StatusOK {
				successCount++
			} else if result.StatusCode == http.StatusUnprocessableEntity {
				failCount++
			}
		}(i)
	}

	wg.Wait()

	// Exactly 10 should succeed (10000 / 1000 = 10)
	if successCount != 10 {
		t.Errorf("expected 10 successful transfers, got %d", successCount)
	}
	if failCount != 10 {
		t.Errorf("expected 10 failed transfers, got %d", failCount)
	}

	// wallet_1 balance should be exactly 0
	var balance int64
	db.QueryRow("SELECT balance FROM wallets WHERE id = ?", "wallet_1").Scan(&balance)
	if balance != 0 {
		t.Errorf("expected wallet_1 balance 0 after draining, got %d", balance)
	}

	// Total balance should be conserved
	var totalBalance int64
	db.QueryRow("SELECT COALESCE(SUM(balance), 0) FROM wallets").Scan(&totalBalance)
	if totalBalance != 25000 {
		t.Errorf("expected total balance 25000, got %d", totalBalance)
	}
}

func TestIdempotency_FailedTransferReplay(t *testing.T) {
	svc, db := testSetup(t)
	ctx := context.Background()

	req := &domain.TransferRequest{
		IdempotencyKey: "test-idempotent-failed",
		FromWalletID:   "wallet_3",
		ToWalletID:     "wallet_2",
		Amount:         99999,
	}

	result1, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("first request error: %v", err)
	}
	if result1.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", result1.StatusCode)
	}

	result2, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("retry error: %v", err)
	}
	if result2.StatusCode != result1.StatusCode {
		t.Errorf("status codes differ: %d vs %d", result1.StatusCode, result2.StatusCode)
	}
	if string(result1.Body) != string(result2.Body) {
		t.Errorf("response bodies differ on retry")
	}

	var transferCount int
	db.QueryRow("SELECT COUNT(*) FROM transfers WHERE idempotency_key = ?", req.IdempotencyKey).Scan(&transferCount)
	if transferCount != 1 {
		t.Errorf("expected 1 transfer record, got %d", transferCount)
	}

	var balance int64
	db.QueryRow("SELECT balance FROM wallets WHERE id = ?", "wallet_3").Scan(&balance)
	if balance != 5000 {
		t.Errorf("expected unchanged balance 5000, got %d", balance)
	}
}

func TestTransferStateTransitions(t *testing.T) {
	svc, db := testSetup(t)
	ctx := context.Background()

	// Test PENDING -> PROCESSED
	req := &domain.TransferRequest{
		IdempotencyKey: "state-success",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}
	result, err := svc.CreateTransfer(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp domain.TransferResponse
	json.Unmarshal(result.Body, &resp)

	var status string
	db.QueryRow("SELECT status FROM transfers WHERE id = ?", resp.ID).Scan(&status)
	if status != string(domain.TransferStatusProcessed) {
		t.Errorf("expected PROCESSED, got %s", status)
	}

	// Test PENDING -> FAILED (insufficient balance)
	req2 := &domain.TransferRequest{
		IdempotencyKey: "state-fail",
		FromWalletID:   "wallet_3", // balance 5000
		ToWalletID:     "wallet_2",
		Amount:         99999,
	}
	result2, err := svc.CreateTransfer(ctx, req2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var resp2 domain.TransferResponse
	json.Unmarshal(result2.Body, &resp2)

	db.QueryRow("SELECT status FROM transfers WHERE id = ?", resp2.ID).Scan(&status)
	if status != string(domain.TransferStatusFailed) {
		t.Errorf("expected FAILED, got %s", status)
	}

	// Verify no transfer is stuck in PENDING
	var pendingCount int
	db.QueryRow("SELECT COUNT(*) FROM transfers WHERE status = 'PENDING'").Scan(&pendingCount)
	if pendingCount != 0 {
		t.Errorf("expected 0 PENDING transfers, got %d", pendingCount)
	}
}
