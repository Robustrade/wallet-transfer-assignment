package test

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/dale-tomson/wallet-transfer-assignment/internal/service"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIdempotency_ConcurrentReplays(t *testing.T) {
	tests := []struct {
		name           string
		concurrency    int
		transferAmount int64
		initialBalance int64
	}{
		{
			name:           "20 concurrent requests with identical key: executes exactly once",
			concurrency:    20,
			transferAmount: 2500,
			initialBalance: 20000,
		},
		{
			name:           "40 concurrent requests with identical key: executes exactly once",
			concurrency:    40,
			transferAmount: 5000,
			initialBalance: 50000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, svc, walletRepo := setupIsolatedTestDB(t)
			ctx := context.Background()

			// Initialize and fund wallets
			walletA := &domain.Wallet{
				ID:        uuid.New().String(),
				UserID:    "user-A",
				Currency:  "USD",
				Balance:   tt.initialBalance,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			walletB := &domain.Wallet{
				ID:        uuid.New().String(),
				UserID:    "user-B",
				Currency:  "USD",
				Balance:   0,
				CreatedAt: time.Now(),
				UpdatedAt: time.Now(),
			}
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, walletA))
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, walletB))

			sharedKey := fmt.Sprintf("shared-key-%s-%s", tt.name, uuid.New().String()[:8])

			var wg sync.WaitGroup
			var completedCount int64
			var conflictCount int64
			var firstTransferID string
			var mu sync.Mutex

			wg.Add(tt.concurrency)
			for i := 0; i < tt.concurrency; i++ {
				go func() {
					defer wg.Done()
					req := service.TransferRequest{
						IdempotencyKey: sharedKey,
						FromWalletID:   walletA.ID,
						ToWalletID:     walletB.ID,
						Amount:         tt.transferAmount,
					}

					resp, err := svc.Transfer(context.Background(), req)
					if err == nil && resp != nil {
						atomic.AddInt64(&completedCount, 1)
						mu.Lock()
						if firstTransferID == "" {
							firstTransferID = resp.TransferID
						} else {
							assert.Equal(t, firstTransferID, resp.TransferID, "All successful replayed responses must share identical transfer ID")
						}
						mu.Unlock()
					} else if errors.Is(err, domain.ErrConcurrentRequest) {
						atomic.AddInt64(&conflictCount, 1)
					}
				}()
			}

			wg.Wait()

			// Every request must either receive the completed transfer (cached replay) or a 409 in-flight conflict
			assert.Equal(t, int64(tt.concurrency), completedCount+conflictCount, "Every concurrent request must resolve cleanly")
			assert.GreaterOrEqual(t, completedCount, int64(1), "At least one request must successfully execute")

			// Critical Invariant: Wallet A must be debited EXACTLY ONCE
			expectedSenderBalance := tt.initialBalance - tt.transferAmount
			updatedA, err := svc.GetWallet(ctx, walletA.ID)
			require.NoError(t, err)
			assert.Equal(t, expectedSenderBalance, updatedA.Balance, "Source wallet must be debited exactly once")

			// Wallet B must be credited EXACTLY ONCE
			updatedB, err := svc.GetWallet(ctx, walletB.ID)
			require.NoError(t, err)
			assert.Equal(t, tt.transferAmount, updatedB.Balance, "Destination wallet must be credited exactly once")

			// Database Audit Trail Assertion: Exactly 1 transfer record and 2 ledger entry rows
			var transferCount int
			err = db.QueryRow("SELECT COUNT(*) FROM transfers WHERE idempotency_key = ?", sharedKey).Scan(&transferCount)
			require.NoError(t, err)
			assert.Equal(t, 1, transferCount, "Exactly one transfer row must be created")

			var ledgerCount int
			err = db.QueryRow("SELECT COUNT(*) FROM ledger_entries WHERE transfer_id = ?", firstTransferID).Scan(&ledgerCount)
			require.NoError(t, err)
			assert.Equal(t, 2, ledgerCount, "Exactly two ledger entries (1 DEBIT, 1 CREDIT) must exist")
		})
	}
}

func TestIdempotency_PayloadMismatchConflict_TableDriven(t *testing.T) {
	tests := []struct {
		name          string
		initialAmount int64
		replayAmount  int64
		alterDest     bool
	}{
		{
			name:          "different amount triggers 422 conflict",
			initialAmount: 500,
			replayAmount:  999,
			alterDest:     false,
		},
		{
			name:          "different destination wallet triggers 422 conflict",
			initialAmount: 500,
			replayAmount:  500,
			alterDest:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, svc, walletRepo := setupIsolatedTestDB(t)
			ctx := context.Background()

			walletA := &domain.Wallet{ID: uuid.New().String(), UserID: "user-A", Currency: "USD", Balance: 10000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			walletB := &domain.Wallet{ID: uuid.New().String(), UserID: "user-B", Currency: "USD", Balance: 0, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			walletC := &domain.Wallet{ID: uuid.New().String(), UserID: "user-C", Currency: "USD", Balance: 0, CreatedAt: time.Now(), UpdatedAt: time.Now()}

			require.NoError(t, walletRepo.CreateWallet(ctx, nil, walletA))
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, walletB))
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, walletC))

			sharedKey := fmt.Sprintf("conflict-key-%s", uuid.New().String()[:8])

			// First transfer succeeds
			req1 := service.TransferRequest{
				IdempotencyKey: sharedKey,
				FromWalletID:   walletA.ID,
				ToWalletID:     walletB.ID,
				Amount:         tt.initialAmount,
			}
			resp1, err := svc.Transfer(ctx, req1)
			require.NoError(t, err)
			require.NotNil(t, resp1)

			// Second transfer with altered payload using the same key must return ErrIdempotencyKeyConflict
			destID := walletB.ID
			if tt.alterDest {
				destID = walletC.ID
			}

			req2 := service.TransferRequest{
				IdempotencyKey: sharedKey,
				FromWalletID:   walletA.ID,
				ToWalletID:     destID,
				Amount:         tt.replayAmount,
			}
			resp2, mismatchErr := svc.Transfer(ctx, req2)
			assert.ErrorIs(t, mismatchErr, domain.ErrIdempotencyKeyConflict, "Reusing key with different payload must fail with conflict error")
			assert.Nil(t, resp2)
		})
	}
}
