package integration

import (
	"context"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/Robustrade/wallet-transfer-assignment/internal/service"
	"gorm.io/gorm"
)

// setupTestDB connects to the dedicated test database or skips if not configured.
func setupTestDB(t *testing.T) (*gorm.DB, repository.WalletRepository, service.TransferService) {
	t.Helper()

	testDBURL := os.Getenv("TEST_DATABASE_URL")
	if testDBURL == "" {
		t.Skip("Skipping PostgreSQL integration test: TEST_DATABASE_URL environment variable is not set. Run with a dedicated test database (e.g. TEST_DATABASE_URL=postgres://postgres:postgres@localhost:5432/wallet_test_db?sslmode=disable)")
	}

	db, err := repository.OpenDatabase(testDBURL, "error")
	if err != nil {
		t.Fatalf("Failed to connect to test database: %v", err)
	}

	// Run auto-migration for test database
	if err := repository.AutoMigrate(db); err != nil {
		t.Fatalf("Failed to migrate test database: %v", err)
	}

	// Clean tables before running test
	cleanTestDatabase(t, db)

	repo := repository.NewGormWalletRepository(db)
	svc := service.NewTransferService(repo)

	return db, repo, svc
}

func cleanTestDatabase(t *testing.T, db *gorm.DB) {
	t.Helper()
	// Clean in order respecting foreign keys
	db.Exec("DELETE FROM idempotency_records")
	db.Exec("DELETE FROM ledger_entries")
	db.Exec("DELETE FROM transfers")
	db.Exec("DELETE FROM wallets")
	db.Exec("DELETE FROM users")
}

func seedUserAndWallets(t *testing.T, db *gorm.DB) {
	t.Helper()
	now := time.Now().UTC()
	user1 := domain.User{ID: "test_user_1", Name: "User 1", CreatedAt: now}
	user2 := domain.User{ID: "test_user_2", Name: "User 2", CreatedAt: now}
	if err := db.Create(&[]domain.User{user1, user2}).Error; err != nil {
		t.Fatalf("failed to seed users: %v", err)
	}

	w1 := domain.Wallet{ID: "wallet_a", UserID: "test_user_1", Balance: 10000, Currency: "INR", CreatedAt: now, UpdatedAt: now}
	w2 := domain.Wallet{ID: "wallet_b", UserID: "test_user_2", Balance: 5000, Currency: "INR", CreatedAt: now, UpdatedAt: now}
	if err := db.Create(&[]domain.Wallet{w1, w2}).Error; err != nil {
		t.Fatalf("failed to seed wallets: %v", err)
	}
}

func TestIntegration_SuccessfulTransfer(t *testing.T) {
	db, repo, svc := setupTestDB(t)
	seedUserAndWallets(t, db)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "integ-success-001",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         4000, // ₹40.00
	}

	res, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("transfer failed: %v", err)
	}

	if res.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 Created, got %d", res.StatusCode)
	}
	if res.Replayed {
		t.Errorf("expected replayed = false for initial transfer")
	}

	// Check balances
	wA, err := repo.GetWallet(context.Background(), "wallet_a")
	if err != nil {
		t.Fatalf("failed to get wallet_a: %v", err)
	}
	wB, err := repo.GetWallet(context.Background(), "wallet_b")
	if err != nil {
		t.Fatalf("failed to get wallet_b: %v", err)
	}

	if wA.Balance != 6000 {
		t.Errorf("expected wallet_a balance 6000, got %d", wA.Balance)
	}
	if wB.Balance != 9000 {
		t.Errorf("expected wallet_b balance 9000, got %d", wB.Balance)
	}

	// Check Double-Entry Ledger
	var ledgers []domain.LedgerEntry
	db.Find(&ledgers)
	if len(ledgers) != 2 {
		t.Fatalf("expected exactly 2 ledger entries, got %d", len(ledgers))
	}

	var debitFound, creditFound bool
	for _, l := range ledgers {
		if l.EntryType == domain.LedgerEntryDebit {
			debitFound = true
			if l.WalletID != "wallet_a" || l.Amount != 4000 {
				t.Errorf("unexpected debit entry: %+v", l)
			}
		}
		if l.EntryType == domain.LedgerEntryCredit {
			creditFound = true
			if l.WalletID != "wallet_b" || l.Amount != 4000 {
				t.Errorf("unexpected credit entry: %+v", l)
			}
		}
	}
	if !debitFound || !creditFound {
		t.Errorf("both debit and credit entries must exist in ledger")
	}
}

func TestIntegration_InsufficientFunds(t *testing.T) {
	db, repo, svc := setupTestDB(t)
	seedUserAndWallets(t, db)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "integ-insufficient-001",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         50000, // Balance is only 10000
	}

	res, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("expected graceful conflict result, got error: %v", err)
	}

	if res.StatusCode != http.StatusConflict {
		t.Errorf("expected status 409 Conflict, got %d", res.StatusCode)
	}

	// Verify balances unchanged
	wA, _ := repo.GetWallet(context.Background(), "wallet_a")
	wB, _ := repo.GetWallet(context.Background(), "wallet_b")
	if wA.Balance != 10000 {
		t.Errorf("expected wallet_a balance 10000, got %d", wA.Balance)
	}
	if wB.Balance != 5000 {
		t.Errorf("expected wallet_b balance 5000, got %d", wB.Balance)
	}

	// Verify no ledger entries created
	var count int64
	db.Model(&domain.LedgerEntry{}).Count(&count)
	if count != 0 {
		t.Errorf("expected 0 ledger entries, got %d", count)
	}

	// Verify transfer status FAILED in database
	var transfers []domain.Transfer
	db.Find(&transfers)
	if len(transfers) != 1 {
		t.Fatalf("expected 1 transfer record, got %d", len(transfers))
	}
	if transfers[0].Status != domain.TransferStatusFailed {
		t.Errorf("expected transfer status FAILED, got %s", transfers[0].Status)
	}
}

func TestIntegration_IdempotencyReplay(t *testing.T) {
	db, repo, svc := setupTestDB(t)
	seedUserAndWallets(t, db)

	req := &service.CreateTransferRequest{
		IdempotencyKey: "integ-replay-001",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         2500,
	}

	// 1. Initial attempt
	res1, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}
	if res1.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d", res1.StatusCode)
	}

	// 2. Replay with identical key
	res2, err := svc.CreateTransfer(context.Background(), req)
	if err != nil {
		t.Fatalf("replayed transfer failed: %v", err)
	}
	if res2.StatusCode != http.StatusCreated {
		t.Errorf("expected 201 Created on replay, got %d", res2.StatusCode)
	}
	if !res2.Replayed {
		t.Errorf("expected replayed to be true")
	}

	// Verify balance decremented exactly once
	wA, _ := repo.GetWallet(context.Background(), "wallet_a")
	if wA.Balance != 7500 {
		t.Errorf("expected balance 7500 after single debit, got %d", wA.Balance)
	}

	// Verify exactly 2 ledger entries in database
	var count int64
	db.Model(&domain.LedgerEntry{}).Count(&count)
	if count != 2 {
		t.Errorf("expected 2 ledger entries, got %d", count)
	}
}

func TestIntegration_IdempotencyPayloadMismatch(t *testing.T) {
	db, _, svc := setupTestDB(t)
	seedUserAndWallets(t, db)

	req1 := &service.CreateTransferRequest{
		IdempotencyKey: "integ-mismatch-key",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         1000,
	}
	_, err := svc.CreateTransfer(context.Background(), req1)
	if err != nil {
		t.Fatalf("first transfer failed: %v", err)
	}

	// Reusing key with different amount must return Conflict error
	req2 := &service.CreateTransferRequest{
		IdempotencyKey: "integ-mismatch-key",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         2000, // Different amount
	}
	_, err = svc.CreateTransfer(context.Background(), req2)
	if err == nil {
		t.Fatalf("expected conflict on key reuse with different parameters, got nil")
	}
	if err != domain.ErrIdempotencyKeyReuseMismatch {
		t.Errorf("expected ErrIdempotencyKeyReuseMismatch, got %v", err)
	}
}

func TestIntegration_ValidationFailures(t *testing.T) {
	db, _, svc := setupTestDB(t)
	seedUserAndWallets(t, db)

	// Same wallet
	_, err := svc.CreateTransfer(context.Background(), &service.CreateTransferRequest{
		IdempotencyKey: "key-v1",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_a",
		Amount:         100,
	})
	if err != domain.ErrSameWalletTransfer {
		t.Errorf("expected ErrSameWalletTransfer, got %v", err)
	}

	// Negative amount
	_, err = svc.CreateTransfer(context.Background(), &service.CreateTransferRequest{
		IdempotencyKey: "key-v2",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_b",
		Amount:         -100,
	})
	if err != domain.ErrInvalidAmount {
		t.Errorf("expected ErrInvalidAmount, got %v", err)
	}

	// Missing wallet
	_, err = svc.CreateTransfer(context.Background(), &service.CreateTransferRequest{
		IdempotencyKey: "key-v3",
		FromWalletID:   "wallet_a",
		ToWalletID:     "wallet_nonexistent",
		Amount:         100,
	})
	if err != domain.ErrWalletNotFound {
		t.Errorf("expected ErrWalletNotFound, got %v", err)
	}
}

// TestIntegration_ConcurrentDebits_NoOverspending tests that concurrent transfers
// cannot spend more than the available balance and that balance never becomes negative.
func TestIntegration_ConcurrentDebits_NoOverspending(t *testing.T) {
	db, repo, svc := setupTestDB(t)

	now := time.Now().UTC()
	u := domain.User{ID: "conc_user", Name: "Concurrency User", CreatedAt: now}
	db.Create(&u)

	// Wallet with 5000 paise (₹50.00)
	wA := domain.Wallet{ID: "wallet_conc_a", UserID: "conc_user", Balance: 5000, Currency: "INR", CreatedAt: now, UpdatedAt: now}
	wB := domain.Wallet{ID: "wallet_conc_b", UserID: "conc_user", Balance: 0, Currency: "INR", CreatedAt: now, UpdatedAt: now}
	db.Create(&wA)
	db.Create(&wB)

	// Launch 10 concurrent transfers of 2000 paise each
	// Only 2 transfers can succeed (2 * 2000 = 4000 <= 5000). The remaining 8 must fail.
	const numRequests = 10
	const transferAmount = 2000

	var wg sync.WaitGroup
	var successCount int32
	var conflictCount int32

	for i := 0; i < numRequests; i++ {
		wg.Add(1)
		key := "conc-transfer-" + string(rune('A'+i))
		go func(idempotencyKey string) {
			defer wg.Done()
			req := &service.CreateTransferRequest{
				IdempotencyKey: idempotencyKey,
				FromWalletID:   "wallet_conc_a",
				ToWalletID:     "wallet_conc_b",
				Amount:         transferAmount,
			}
			res, err := svc.CreateTransfer(context.Background(), req)
			if err != nil {
				return
			}
			if res.StatusCode == http.StatusCreated {
				atomic.AddInt32(&successCount, 1)
			} else if res.StatusCode == http.StatusConflict {
				atomic.AddInt32(&conflictCount, 1)
			}
		}(key)
	}

	wg.Wait()

	if successCount != 2 {
		t.Errorf("expected exactly 2 successful transfers, got %d", successCount)
	}
	if conflictCount != 8 {
		t.Errorf("expected exactly 8 rejected transfers, got %d", conflictCount)
	}

	// Verify final balances
	finalA, _ := repo.GetWallet(context.Background(), "wallet_conc_a")
	finalB, _ := repo.GetWallet(context.Background(), "wallet_conc_b")

	if finalA.Balance != 1000 {
		t.Errorf("expected wallet A balance 1000, got %d", finalA.Balance)
	}
	if finalB.Balance != 4000 {
		t.Errorf("expected wallet B balance 4000, got %d", finalB.Balance)
	}
	if finalA.Balance < 0 {
		t.Fatalf("CRITICAL: wallet balance became negative: %d", finalA.Balance)
	}

	// Verify total ledger entries count = exactly 4 (2 debits + 2 credits)
	var ledgerCount int64
	db.Model(&domain.LedgerEntry{}).Count(&ledgerCount)
	if ledgerCount != 4 {
		t.Errorf("expected exactly 4 ledger entries, got %d", ledgerCount)
	}
}

// TestIntegration_ConcurrentDuplicateRequests tests that simultaneous requests
// using the EXACT same idempotency key execute only once and all return the same response.
func TestIntegration_ConcurrentDuplicateRequests(t *testing.T) {
	db, repo, svc := setupTestDB(t)
	seedUserAndWallets(t, db)

	const numGoroutines = 10
	const sharedKey = "shared-concurrent-key-999"

	var wg sync.WaitGroup
	var successCount int32
	var bodies []string
	var mu sync.Mutex

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req := &service.CreateTransferRequest{
				IdempotencyKey: sharedKey,
				FromWalletID:   "wallet_a",
				ToWalletID:     "wallet_b",
				Amount:         1500,
			}
			res, err := svc.CreateTransfer(context.Background(), req)
			if err != nil {
				t.Errorf("concurrent transfer failed: %v", err)
				return
			}
			if res.StatusCode == http.StatusCreated {
				atomic.AddInt32(&successCount, 1)
				mu.Lock()
				bodies = append(bodies, string(res.ResponseBody))
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if successCount != numGoroutines {
		t.Errorf("expected all %d goroutines to receive 201 Created, got %d", numGoroutines, successCount)
	}

	// Verify all returned response bodies are identical
	if len(bodies) > 0 {
		first := bodies[0]
		for _, b := range bodies[1:] {
			if b != first {
				t.Errorf("response bodies diverged between concurrent callers!")
			}
		}
	}

	// Verify balance decremented ONLY ONCE: 10000 - 1500 = 8500
	wA, _ := repo.GetWallet(context.Background(), "wallet_a")
	if wA.Balance != 8500 {
		t.Errorf("expected wallet_a balance 8500, got %d (debit duplicated!)", wA.Balance)
	}

	// Verify only 2 ledger entries exist in database
	var ledgerCount int64
	db.Model(&domain.LedgerEntry{}).Count(&ledgerCount)
	if ledgerCount != 2 {
		t.Errorf("expected exactly 2 ledger entries, got %d", ledgerCount)
	}
}

func TestIntegration_TransactionRollback(t *testing.T) {
	db, repo, _ := setupTestDB(t)
	seedUserAndWallets(t, db)

	// Simulate a transaction failure: debit succeeds, but an unhandled constraint fails
	err := repo.WithTransaction(context.Background(), func(txRepo repository.WalletRepository) error {
		// Debit wallet_a
		rows, err := txRepo.DebitWalletConditional(context.Background(), "wallet_a", 2000)
		if err != nil || rows == 0 {
			return err
		}

		// Force an error to abort transaction
		return domain.NewDomainError(domain.CodeInternalError, "simulated unexpected error to trigger rollback")
	})

	if err == nil {
		t.Fatalf("expected error from transaction, got nil")
	}

	// Verify wallet_a balance is still 10000 (not 8000)
	wA, _ := repo.GetWallet(context.Background(), "wallet_a")
	if wA.Balance != 10000 {
		t.Errorf("expected balance to remain 10000 after rollback, got %d", wA.Balance)
	}

	// Verify no ledger entries created
	var ledgerCount int64
	db.Model(&domain.LedgerEntry{}).Count(&ledgerCount)
	if ledgerCount != 0 {
		t.Errorf("expected 0 ledger entries, got %d", ledgerCount)
	}
}
