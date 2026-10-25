package test

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/repository"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupIsolatedTestDB creates a clean, isolated SQLite test database
func setupIsolatedTestDB(t *testing.T) (*sql.DB, service.WalletService, repository.WalletRepository) {
	t.Helper()
	tmpDir := t.TempDir()
	dbFile := filepath.Join(tmpDir, fmt.Sprintf("test_concurrency_%s.db", uuid.New().String()))

	db, err := repository.NewSQLiteDB(dbFile)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	txManager := repository.NewSQLTxManager(db)
	walletRepo := repository.NewSQLWalletRepository(db)
	transferRepo := repository.NewSQLTransferRepository(db)
	idempotencyRepo := repository.NewSQLIdempotencyRepository(db)

	svc := service.NewWalletService(txManager, walletRepo, transferRepo, idempotencyRepo)

	return db, svc, walletRepo
}

func TestConcurrency_ParallelOverdraftProtection(t *testing.T) {
	tests := []struct {
		name            string
		initialBalance  int64
		transferAmount  int64
		numGoroutines   int
		expectedSuccess int64
		expectedFailure int64
	}{
		{
			name:            "50 concurrent debits of $10 from $100 account: exactly 10 succeed, 40 fail",
			initialBalance:  10000,
			transferAmount:  1000,
			numGoroutines:   50,
			expectedSuccess: 10,
			expectedFailure: 40,
		},
		{
			name:            "20 concurrent debits of $50 from $100 account: exactly 2 succeed, 18 fail",
			initialBalance:  10000,
			transferAmount:  5000,
			numGoroutines:   20,
			expectedSuccess: 2,
			expectedFailure: 18,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, svc, walletRepo := setupIsolatedTestDB(t)
			ctx := context.Background()

			// 1. Create and fund source Wallet A
			walletA := &domain.Wallet{
				ID:        uuid.New().String(),
				UserID:    "user-A",
				Currency:  "USD",
				Balance:   tt.initialBalance,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, walletA))

			// 2. Create destination Wallet B with 0 balance
			walletB := &domain.Wallet{
				ID:        uuid.New().String(),
				UserID:    "user-B",
				Currency:  "USD",
				Balance:   0,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, walletB))

			// 3. Concurrently trigger parallel transfer attempts
			var wg sync.WaitGroup
			var successCount int64
			var failureCount int64

			wg.Add(tt.numGoroutines)
			for i := 0; i < tt.numGoroutines; i++ {
				key := fmt.Sprintf("key-overdraft-%s-%d", tt.name, i)
				go func(idempotencyKey string) {
					defer wg.Done()

					req := service.TransferRequest{
						IdempotencyKey: idempotencyKey,
						FromWalletID:   walletA.ID,
						ToWalletID:     walletB.ID,
						Amount:         tt.transferAmount,
					}

					resp, err := svc.Transfer(context.Background(), req)
					if err == nil && resp != nil && resp.Status == "COMPLETED" {
						atomic.AddInt64(&successCount, 1)
					} else {
						atomic.AddInt64(&failureCount, 1)
					}
				}(key)
			}

			wg.Wait()

			// 4. Validate exact execution counts
			assert.Equal(t, tt.expectedSuccess, successCount, "Expected exact success count")
			assert.Equal(t, tt.expectedFailure, failureCount, "Expected exact failure count")

			// 5. Verify source wallet non-negative balance
			expectedRemaining := tt.initialBalance - (tt.expectedSuccess * tt.transferAmount)
			updatedA, err := svc.GetWallet(ctx, walletA.ID)
			require.NoError(t, err)
			assert.Equal(t, expectedRemaining, updatedA.Balance, "Source balance must equal initial minus total debited")
			assert.GreaterOrEqual(t, updatedA.Balance, int64(0), "Balance must never drop below zero")

			// 6. Verify destination wallet received exact sum
			expectedCredited := tt.expectedSuccess * tt.transferAmount
			updatedB, err := svc.GetWallet(ctx, walletB.ID)
			require.NoError(t, err)
			assert.Equal(t, expectedCredited, updatedB.Balance, "Destination balance must equal total credited")

			// 7. Verify conservation of money
			assert.Equal(t, tt.initialBalance, updatedA.Balance+updatedB.Balance, "Total money across wallets must be strictly conserved")

			// 8. Double-Entry Ledger Invariant Assertion: sum(debit) == sum(credit)
			var totalDebits, totalCredits int64
			err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE direction = 'DEBIT'").Scan(&totalDebits)
			require.NoError(t, err)
			err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE direction = 'CREDIT'").Scan(&totalCredits)
			require.NoError(t, err)

			assert.Equal(t, totalDebits, totalCredits, "Total ledger debits must balance total ledger credits")
			assert.Equal(t, expectedCredited, totalDebits)
		})
	}
}

func TestConcurrency_BidirectionalDeadlockFreedom(t *testing.T) {
	tests := []struct {
		name                  string
		initialBalanceA       int64
		initialBalanceB       int64
		transfersPerDirection int
		transferAmount        int64
	}{
		{
			name:                  "50 concurrent bidirectional transfers (25 A->B, 25 B->A)",
			initialBalanceA:       50000,
			initialBalanceB:       50000,
			transfersPerDirection: 25,
			transferAmount:        100,
		},
		{
			name:                  "100 concurrent bidirectional transfers (50 A->B, 50 B->A)",
			initialBalanceA:       100000,
			initialBalanceB:       100000,
			transfersPerDirection: 50,
			transferAmount:        500,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, svc, walletRepo := setupIsolatedTestDB(t)
			ctx := context.Background()

			// Seed Wallet A and Wallet B
			wA := &domain.Wallet{
				ID:        uuid.New().String(),
				UserID:    "user-A",
				Currency:  "USD",
				Balance:   tt.initialBalanceA,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			wB := &domain.Wallet{
				ID:        uuid.New().String(),
				UserID:    "user-B",
				Currency:  "USD",
				Balance:   tt.initialBalanceB,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, wA))
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, wB))

			var wg sync.WaitGroup
			totalTransfers := tt.transfersPerDirection * 2
			wg.Add(totalTransfers)

			// A -> B concurrent transfers
			for i := 0; i < tt.transfersPerDirection; i++ {
				key := fmt.Sprintf("bidirectional-A-to-B-%d-%s", i, uuid.New().String()[:8])
				go func(idempotencyKey string) {
					defer wg.Done()
					req := service.TransferRequest{
						IdempotencyKey: idempotencyKey,
						FromWalletID:   wA.ID,
						ToWalletID:     wB.ID,
						Amount:         tt.transferAmount,
					}
					_, _ = svc.Transfer(context.Background(), req)
				}(key)
			}

			// B -> A concurrent transfers
			for i := 0; i < tt.transfersPerDirection; i++ {
				key := fmt.Sprintf("bidirectional-B-to-A-%d-%s", i, uuid.New().String()[:8])
				go func(idempotencyKey string) {
					defer wg.Done()
					req := service.TransferRequest{
						IdempotencyKey: idempotencyKey,
						FromWalletID:   wB.ID,
						ToWalletID:     wA.ID,
						Amount:         tt.transferAmount,
					}
					_, _ = svc.Transfer(context.Background(), req)
				}(key)
			}

			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			// Must complete without thread lockup or deadlock within 10 seconds
			select {
			case <-done:
				// Successfully completed without deadlock
			case <-time.After(10 * time.Second):
				t.Fatal("Bidirectional transfer test timed out: Deadlock detected")
			}

			// Invariant Check: Sum of both balances must remain exactly initial total
			updatedA, err := svc.GetWallet(ctx, wA.ID)
			require.NoError(t, err)
			updatedB, err := svc.GetWallet(ctx, wB.ID)
			require.NoError(t, err)

			initialTotal := tt.initialBalanceA + tt.initialBalanceB
			finalTotal := updatedA.Balance + updatedB.Balance
			assert.Equal(t, initialTotal, finalTotal, "Total money across wallets must be strictly conserved")

			// Check ledger conservation
			var totalDebits, totalCredits int64
			err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE direction = 'DEBIT'").Scan(&totalDebits)
			require.NoError(t, err)
			err = db.QueryRow("SELECT COALESCE(SUM(amount), 0) FROM ledger_entries WHERE direction = 'CREDIT'").Scan(&totalCredits)
			require.NoError(t, err)
			assert.Equal(t, totalDebits, totalCredits, "Total ledger debits must balance credits")
		})
	}
}
