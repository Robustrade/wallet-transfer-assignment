package repository_test

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"github.com/google/uuid"
	_ "github.com/lib/pq"
)

func getDB(t *testing.T) *sql.DB {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatalf("failed to connect to db: %v", err)
	}

	// Limit connections so we don't blow up the free tier Aiven DB limits during high concurrency execution
	db.SetMaxOpenConns(5)
	db.SetMaxIdleConns(5)

	return db
}

func TestConcurrency_DoubleSpendAndIdempotency(t *testing.T) {
	db := getDB(t)
	defer db.Close()

	repo := repository.NewPostgresRepository(db)
	ctx := context.Background()

	w1 := &model.Wallet{ID: "wallet_" + uuid.New().String(), Balance: 1000}
	w2 := &model.Wallet{ID: "wallet_" + uuid.New().String(), Balance: 0}

	if err := repo.CreateWallet(ctx, w1); err != nil {
		t.Fatalf("failed to map wallet 1: %v", err)
	}
	if err := repo.CreateWallet(ctx, w2); err != nil {
		t.Fatalf("failed to map wallet 2: %v", err)
	}

	// 1. Concurrency double spending test:
	// We will try to transfer 100 from w1 to w2, 20 times concurrently.
	// We only have 1000 balance, so it should succeed exactly 10 times, and fail 10 times with insufficient funds.
	t.Run("double_spend_prevention", func(t *testing.T) {
		var wg sync.WaitGroup
		successCount := 0
		failCount := 0
		var mu sync.Mutex

		for i := 0; i < 20; i++ {
			wg.Add(1)
			go func(iteration int) {
				defer wg.Done()
				transfer := &model.Transfer{
					ID:             uuid.New().String(),
					IdempotencyKey: fmt.Sprintf("idem_%s_%d", w1.ID, iteration),
					FromWalletID:   w1.ID,
					ToWalletID:     w2.ID,
					Amount:         100,
				}

				err := repo.ExecuteTransferTx(ctx, transfer)
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					successCount++
				} else if err == repository.ErrInsufficientFunds {
					failCount++
				} else {
					t.Errorf("unexpected error: %v", err)
				}
			}(i)
		}
		wg.Wait()

		if successCount != 10 {
			t.Errorf("Expected exactly 10 successful transfers, got %d", successCount)
		}
		if failCount != 10 {
			t.Errorf("Expected exactly 10 failed transfers, got %d", failCount)
		}

		// Verify final balances
		fw1, _ := repo.GetWallet(ctx, w1.ID)
		fw2, _ := repo.GetWallet(ctx, w2.ID)

		if fw1.Balance != 0 {
			t.Errorf("Expected w1 balance 0, got %d", fw1.Balance)
		}
		if fw2.Balance != 1000 {
			t.Errorf("Expected w2 balance 1000, got %d", fw2.Balance)
		}
	})

	// 2. Concurrency Idempotency test:
	// 5 identical requests at the exact same time
	t.Run("concurrent_idempotency", func(t *testing.T) {
		idempotencyKey := "idem_same_" + uuid.New().String()
		var wg sync.WaitGroup

		successCount := 0
		conflictCount := 0
		var mu sync.Mutex

		for i := 0; i < 5; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				transfer := &model.Transfer{
					ID:             uuid.New().String(),
					IdempotencyKey: idempotencyKey,
					FromWalletID:   w2.ID, // w2 has 1000 from previous test
					ToWalletID:     w1.ID,
					Amount:         50,
				}

				err := repo.ExecuteTransferTx(ctx, transfer)
				mu.Lock()
				defer mu.Unlock()
				if err == nil {
					successCount++
				} else if err == repository.ErrIdempotencyConflict {
					conflictCount++
				} else {
					t.Errorf("unexpected error on idempotent transfer: %v", err)
				}
			}()
		}
		wg.Wait()

		if successCount != 1 {
			t.Errorf("Expected exactly 1 successful execution, got %d", successCount)
		}
		if conflictCount != 4 {
			t.Errorf("Expected exactly 4 idempotency conflict rejections locally within repo, got %d", conflictCount)
		}
	})
}
