package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/dale-tomson/wallet-transfer-assignment/internal/domain"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupTestDB(t *testing.T) (*SQLTxManager, *SQLWalletRepository, *SQLTransferRepository, *SQLIdempotencyRepository, *sql.DB) {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, fmt.Sprintf("test_repo_%s.db", uuid.NewString()))

	db, err := NewSQLiteDB(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	return NewSQLTxManager(db),
		NewSQLWalletRepository(db),
		NewSQLTransferRepository(db),
		NewSQLIdempotencyRepository(db),
		db
}

func TestNewSQLiteDB(t *testing.T) {
	_, _, _, _, db := setupTestDB(t)

	tests := []struct {
		name      string
		tableName string
	}{
		{name: "wallets table exists", tableName: "wallets"},
		{name: "transfers table exists", tableName: "transfers"},
		{name: "ledger_entries table exists", tableName: "ledger_entries"},
		{name: "idempotency_records table exists", tableName: "idempotency_records"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var name string
			err := db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name=?", tt.tableName).Scan(&name)
			require.NoError(t, err)
			assert.Equal(t, tt.tableName, name)
		})
	}
}

func TestUpdateBalances_TableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name          string
		setupWallets  func() (from, to *domain.Wallet)
		transferAmt   int64
		expectedError error
		assertAfter   func(t *testing.T, wRepo *SQLWalletRepository, fromID, toID string)
	}{
		{
			name: "success: valid transfer between wallets",
			setupWallets: func() (*domain.Wallet, *domain.Wallet) {
				return &domain.Wallet{ID: uuid.NewString(), UserID: "u1", Currency: "USD", Balance: 10000, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					&domain.Wallet{ID: uuid.NewString(), UserID: "u2", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			},
			transferAmt:   3000,
			expectedError: nil,
			assertAfter: func(t *testing.T, wRepo *SQLWalletRepository, fromID, toID string) {
				w1, err := wRepo.GetWalletForUpdate(ctx, nil, fromID)
				require.NoError(t, err)
				assert.Equal(t, int64(7000), w1.Balance)

				w2, err := wRepo.GetWalletForUpdate(ctx, nil, toID)
				require.NoError(t, err)
				assert.Equal(t, int64(8000), w2.Balance)
			},
		},
		{
			name: "failure: insufficient funds triggers check constraint",
			setupWallets: func() (*domain.Wallet, *domain.Wallet) {
				return &domain.Wallet{ID: uuid.NewString(), UserID: "u1", Currency: "USD", Balance: 1000, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					&domain.Wallet{ID: uuid.NewString(), UserID: "u2", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			},
			transferAmt:   2000,
			expectedError: domain.ErrInsufficientFunds,
			assertAfter: func(t *testing.T, wRepo *SQLWalletRepository, fromID, toID string) {
				w1, err := wRepo.GetWalletForUpdate(ctx, nil, fromID)
				require.NoError(t, err)
				assert.Equal(t, int64(1000), w1.Balance)
			},
		},
		{
			name: "failure: self transfer to same wallet",
			setupWallets: func() (*domain.Wallet, *domain.Wallet) {
				w := &domain.Wallet{ID: uuid.NewString(), UserID: "u1", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
				return w, w
			},
			transferAmt:   1000,
			expectedError: domain.ErrSelfTransfer,
			assertAfter:   nil,
		},
		{
			name: "failure: zero transfer amount",
			setupWallets: func() (*domain.Wallet, *domain.Wallet) {
				return &domain.Wallet{ID: uuid.NewString(), UserID: "u1", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					&domain.Wallet{ID: uuid.NewString(), UserID: "u2", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			},
			transferAmt:   0,
			expectedError: domain.ErrInvalidAmount,
			assertAfter:   nil,
		},
		{
			name: "failure: negative transfer amount",
			setupWallets: func() (*domain.Wallet, *domain.Wallet) {
				return &domain.Wallet{ID: uuid.NewString(), UserID: "u1", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()},
					&domain.Wallet{ID: uuid.NewString(), UserID: "u2", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			},
			transferAmt:   -500,
			expectedError: domain.ErrInvalidAmount,
			assertAfter:   nil,
		},
		{
			name: "failure: non-existent sender wallet",
			setupWallets: func() (*domain.Wallet, *domain.Wallet) {
				return &domain.Wallet{ID: uuid.NewString(), UserID: "missing", Currency: "USD", Balance: 5000},
					&domain.Wallet{ID: uuid.NewString(), UserID: "u2", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			},
			transferAmt:   1000,
			expectedError: domain.ErrWalletNotFound,
			assertAfter:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txMgr, walletRepo, _, _, _ := setupTestDB(t)

			from, to := tt.setupWallets()
			if from.UserID != "missing" {
				require.NoError(t, walletRepo.CreateWallet(ctx, nil, from))
			}
			if to != from {
				require.NoError(t, walletRepo.CreateWallet(ctx, nil, to))
			}

			err := txMgr.RunInTx(ctx, func(tx *sql.Tx) error {
				return walletRepo.UpdateBalances(ctx, tx, from.ID, to.ID, tt.transferAmt)
			})

			if tt.expectedError != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, tt.expectedError), "expected %v, got %v", tt.expectedError, err)
			} else {
				require.NoError(t, err)
			}

			if tt.assertAfter != nil {
				tt.assertAfter(t, walletRepo, from.ID, to.ID)
			}
		})
	}
}

func TestTransactionAtomicity_TableDriven(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name         string
		action       func(ctx context.Context, tx *sql.Tx, wRepo *SQLWalletRepository, fromID, toID string) error
		shouldCommit bool
	}{
		{
			name: "commit: success updates both balances",
			action: func(ctx context.Context, tx *sql.Tx, wRepo *SQLWalletRepository, fromID, toID string) error {
				return wRepo.UpdateBalances(ctx, tx, fromID, toID, 2000)
			},
			shouldCommit: true,
		},
		{
			name: "rollback: explicit error rolls back balance update",
			action: func(ctx context.Context, tx *sql.Tx, wRepo *SQLWalletRepository, fromID, toID string) error {
				if err := wRepo.UpdateBalances(ctx, tx, fromID, toID, 2000); err != nil {
					return err
				}
				return errors.New("deliberate failure trigger")
			},
			shouldCommit: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			txMgr, walletRepo, _, _, _ := setupTestDB(t)

			w1 := &domain.Wallet{ID: uuid.NewString(), UserID: "u1", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			w2 := &domain.Wallet{ID: uuid.NewString(), UserID: "u2", Currency: "USD", Balance: 5000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, w1))
			require.NoError(t, walletRepo.CreateWallet(ctx, nil, w2))

			err := txMgr.RunInTx(ctx, func(tx *sql.Tx) error {
				return tt.action(ctx, tx, walletRepo, w1.ID, w2.ID)
			})

			if tt.shouldCommit {
				require.NoError(t, err)
				w1After, _ := walletRepo.GetWalletForUpdate(ctx, nil, w1.ID)
				assert.Equal(t, int64(3000), w1After.Balance)
				w2After, _ := walletRepo.GetWalletForUpdate(ctx, nil, w2.ID)
				assert.Equal(t, int64(7000), w2After.Balance)
			} else {
				require.Error(t, err)
				w1After, _ := walletRepo.GetWalletForUpdate(ctx, nil, w1.ID)
				assert.Equal(t, int64(5000), w1After.Balance)
				w2After, _ := walletRepo.GetWalletForUpdate(ctx, nil, w2.ID)
				assert.Equal(t, int64(5000), w2After.Balance)
			}
		})
	}
}

func TestDoubleEntryInvariant(t *testing.T) {
	ctx := context.Background()
	txMgr, walletRepo, transferRepo, _, _ := setupTestDB(t)

	w1 := &domain.Wallet{ID: uuid.NewString(), UserID: "sender", Currency: "USD", Balance: 10000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	w2 := &domain.Wallet{ID: uuid.NewString(), UserID: "receiver", Currency: "USD", Balance: 2000, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	require.NoError(t, walletRepo.CreateWallet(ctx, nil, w1))
	require.NoError(t, walletRepo.CreateWallet(ctx, nil, w2))

	transferAmount := int64(3500)
	transferID := uuid.NewString()

	transfer := &domain.Transfer{
		ID:             transferID,
		IdempotencyKey: "key_" + uuid.NewString(),
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         transferAmount,
		Currency:       "USD",
		Status:         domain.TransferStatusCompleted,
		CreatedAt:      time.Now(),
	}

	debit := &domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transferID,
		WalletID:   w1.ID,
		Amount:     transferAmount,
		Direction:  domain.DirectionDebit,
		CreatedAt:  time.Now(),
	}

	credit := &domain.LedgerEntry{
		ID:         uuid.NewString(),
		TransferID: transferID,
		WalletID:   w2.ID,
		Amount:     transferAmount,
		Direction:  domain.DirectionCredit,
		CreatedAt:  time.Now(),
	}

	err := txMgr.RunInTx(ctx, func(tx *sql.Tx) error {
		if err := walletRepo.UpdateBalances(ctx, tx, w1.ID, w2.ID, transferAmount); err != nil {
			return err
		}
		return transferRepo.CreateTransferAndEntries(ctx, tx, transfer, debit, credit)
	})
	require.NoError(t, err)

	w1After, _ := walletRepo.GetWalletForUpdate(ctx, nil, w1.ID)
	w2After, _ := walletRepo.GetWalletForUpdate(ctx, nil, w2.ID)

	// Net balance across accounts is strictly preserved
	assert.Equal(t, w1.Balance+w2.Balance, w1After.Balance+w2After.Balance)
	assert.Equal(t, int64(6500), w1After.Balance)
	assert.Equal(t, int64(5500), w2After.Balance)
}

func TestIdempotencyLifecycle_TableDriven(t *testing.T) {
	ctx := context.Background()
	_, _, _, idempRepo, _ := setupTestDB(t)

	key := "idemp_test_" + uuid.NewString()
	hashA := "hash_payload_alpha"
	hashB := "hash_payload_beta"

	steps := []struct {
		name          string
		action        func() error
		expectedError error
		validate      func(t *testing.T)
	}{
		{
			name: "1. initial reservation marks IN_PROGRESS",
			action: func() error {
				rec, err := idempRepo.Reserve(ctx, key, hashA, 10*time.Second)
				if err != nil {
					return err
				}
				if rec.Status != domain.IdempotencyStatusInProgress {
					return fmt.Errorf("expected IN_PROGRESS, got %s", rec.Status)
				}
				return nil
			},
			expectedError: nil,
		},
		{
			name: "2. concurrent active lease returns ErrConcurrentRequest",
			action: func() error {
				_, err := idempRepo.Reserve(ctx, key, hashA, 10*time.Second)
				return err
			},
			expectedError: domain.ErrConcurrentRequest,
		},
		{
			name: "3. resolution saves response status and payload",
			action: func() error {
				return idempRepo.Resolve(ctx, nil, key, 200, `{"status":"COMPLETED"}`)
			},
			expectedError: nil,
		},
		{
			name: "4. matching replayed request returns cached response",
			action: func() error {
				rec, err := idempRepo.Reserve(ctx, key, hashA, 10*time.Second)
				if err != nil {
					return err
				}
				if rec.Status != domain.IdempotencyStatusResolved || rec.ResponseCode != 200 {
					return fmt.Errorf("unexpected record: %+v", rec)
				}
				return nil
			},
			expectedError: nil,
		},
		{
			name: "5. replayed request with altered payload returns ErrIdempotencyKeyConflict",
			action: func() error {
				_, err := idempRepo.Reserve(ctx, key, hashB, 10*time.Second)
				return err
			},
			expectedError: domain.ErrIdempotencyKeyConflict,
		},
	}

	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			err := step.action()
			if step.expectedError != nil {
				require.Error(t, err)
				assert.True(t, errors.Is(err, step.expectedError), "expected %v, got %v", step.expectedError, err)
			} else {
				require.NoError(t, err)
			}
			if step.validate != nil {
				step.validate(t)
			}
		})
	}
}

func TestIdempotencyCrashRecoveryAndRetry_TableDriven(t *testing.T) {
	ctx := context.Background()
	_, _, _, idempRepo, _ := setupTestDB(t)

	tests := []struct {
		name   string
		run    func(t *testing.T, idempRepo *SQLIdempotencyRepository)
	}{
		{
			name: "crash recovery: expired lease can be reclaimed as IN_PROGRESS",
			run: func(t *testing.T, idempRepo *SQLIdempotencyRepository) {
				crashedKey := "crashed_" + uuid.NewString()
				// Negative TTL simulates past lease expiration
				rec, err := idempRepo.Reserve(ctx, crashedKey, "hash_crash", -1*time.Second)
				require.NoError(t, err)
				assert.Equal(t, domain.IdempotencyStatusInProgress, rec.Status)

				// Next reservation should reclaim expired lease
				reclaimed, err := idempRepo.Reserve(ctx, crashedKey, "hash_crash", 5*time.Second)
				require.NoError(t, err)
				assert.Equal(t, domain.IdempotencyStatusInProgress, reclaimed.Status)
			},
		},
		{
			name: "failed retry: released/failed key can be safely retried",
			run: func(t *testing.T, idempRepo *SQLIdempotencyRepository) {
				failedKey := "failed_" + uuid.NewString()
				_, err := idempRepo.Reserve(ctx, failedKey, "hash_failed", 5*time.Second)
				require.NoError(t, err)

				err = idempRepo.Release(ctx, failedKey)
				require.NoError(t, err)

				retried, err := idempRepo.Reserve(ctx, failedKey, "hash_failed", 5*time.Second)
				require.NoError(t, err)
				assert.Equal(t, domain.IdempotencyStatusInProgress, retried.Status)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.run(t, idempRepo)
		})
	}
}
