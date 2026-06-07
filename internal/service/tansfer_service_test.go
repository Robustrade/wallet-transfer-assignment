package service_test

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Saket121/wallet-transfer-assignment/internal/domain"
	"github.com/Saket121/wallet-transfer-assignment/internal/repository"
	"github.com/Saket121/wallet-transfer-assignment/internal/service"
)

// newTestDB creates a fresh SQLite file-based database with the schema applied.
// File-based SQLite with WAL mode supports concurrent reads while a write transaction is active.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	// Use a temp file so WAL mode works properly for concurrency
	tmpFile := filepath.Join(t.TempDir(), "test.db")
	dsn := fmt.Sprintf("%s?_busy_timeout=5000&_journal_mode=WAL", tmpFile)
	db, err := sql.Open("sqlite3", dsn)
	require.NoError(t, err)

	// Enable foreign keys and WAL mode for better concurrency
	_, err = db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode = WAL; PRAGMA busy_timeout = 5000")
	require.NoError(t, err)

	// Read and apply migration
	_, filename, _, _ := runtime.Caller(0)
	projectRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	migrationFile := filepath.Join(projectRoot, "db", "migrations", "001_initial_schema.sql")

	migrationSQL, err := os.ReadFile(migrationFile)
	require.NoError(t, err)

	_, err = db.Exec(string(migrationSQL))
	require.NoError(t, err)

	return db
}

func setupService(t *testing.T) (*service.TransferService, *sql.DB) {
	t.Helper()
	db := newTestDB(t)
	repo := repository.NewSQLiteRepository(db)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	svc := service.NewTransferService(repo, logger)
	return svc, db
}

func TestCreateTransfer_Success(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "test-success-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         500,
	}

	result, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.False(t, result.Idempotent)
	assert.Equal(t, domain.TransferStatusProcessed, result.Transfer.Status)
	assert.Equal(t, "wallet_1", result.Transfer.FromWalletID)
	assert.Equal(t, "wallet_2", result.Transfer.ToWalletID)
	assert.Equal(t, int64(500), result.Transfer.Amount)
	assert.NotEmpty(t, result.Transfer.ID)
}

func TestCreateTransfer_InsufficientFunds(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "test-insufficient-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         20000,
	}

	result, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, domain.TransferStatusFailed, result.Transfer.Status)
	assert.Equal(t, "insufficient funds", result.Transfer.FailureReason)
}

func TestCreateTransfer_Idempotency(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "test-idempotency-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         100,
	}

	// First request
	result1, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	assert.False(t, result1.Idempotent)
	assert.Equal(t, domain.TransferStatusProcessed, result1.Transfer.Status)

	// Duplicate request — should return same result without re-executing
	result2, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	assert.True(t, result2.Idempotent)
	assert.Equal(t, result1.Transfer.ID, result2.Transfer.ID)
	assert.Equal(t, result1.Transfer.Status, result2.Transfer.Status)
}

func TestCreateTransfer_ValidationErrors(t *testing.T) {
	svc, _ := setupService(t)
	ctx := context.Background()

	tests := []struct {
		name string
		req  service.TransferRequest
	}{
		{
			name: "missing idempotency key",
			req:  service.TransferRequest{FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 100},
		},
		{
			name: "same wallet",
			req:  service.TransferRequest{IdempotencyKey: "k1", FromWalletID: "wallet_1", ToWalletID: "wallet_1", Amount: 100},
		},
		{
			name: "zero amount",
			req:  service.TransferRequest{IdempotencyKey: "k2", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: 0},
		},
		{
			name: "negative amount",
			req:  service.TransferRequest{IdempotencyKey: "k3", FromWalletID: "wallet_1", ToWalletID: "wallet_2", Amount: -100},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := svc.CreateTransfer(ctx, tt.req)
			assert.Error(t, err)
			assert.Nil(t, result)
		})
	}
}

func TestCreateTransfer_LedgerConsistency(t *testing.T) {
	svc, db := setupService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "test-ledger-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         300,
	}

	result, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)
	require.Equal(t, domain.TransferStatusProcessed, result.Transfer.Status)

	// Verify exactly two ledger entries exist for this transfer
	rows, err := db.QueryContext(ctx,
		`SELECT wallet_id, entry_type, amount FROM ledger_entries WHERE transfer_id = ? ORDER BY entry_type`,
		result.Transfer.ID,
	)
	require.NoError(t, err)
	defer rows.Close()

	type entry struct {
		WalletID  string
		EntryType string
		Amount    int64
	}
	var entries []entry
	for rows.Next() {
		var e entry
		require.NoError(t, rows.Scan(&e.WalletID, &e.EntryType, &e.Amount))
		entries = append(entries, e)
	}

	require.Len(t, entries, 2)
	// CREDIT entry
	assert.Equal(t, "wallet_2", entries[0].WalletID)
	assert.Equal(t, "CREDIT", entries[0].EntryType)
	assert.Equal(t, int64(300), entries[0].Amount)
	// DEBIT entry
	assert.Equal(t, "wallet_1", entries[1].WalletID)
	assert.Equal(t, "DEBIT", entries[1].EntryType)
	assert.Equal(t, int64(300), entries[1].Amount)
}

func TestCreateTransfer_BalanceUpdates(t *testing.T) {
	svc, db := setupService(t)
	ctx := context.Background()

	req := service.TransferRequest{
		IdempotencyKey: "test-balance-1",
		FromWalletID:   "wallet_1",
		ToWalletID:     "wallet_2",
		Amount:         1000,
	}

	_, err := svc.CreateTransfer(ctx, req)
	require.NoError(t, err)

	// Check balances after transfer
	var balance1, balance2 int64
	err = db.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = 'wallet_1'`).Scan(&balance1)
	require.NoError(t, err)
	err = db.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = 'wallet_2'`).Scan(&balance2)
	require.NoError(t, err)

	assert.Equal(t, int64(9000), balance1) // 10000 - 1000
	assert.Equal(t, int64(6000), balance2) // 5000 + 1000
}

func TestCreateTransfer_ConcurrentTransfers(t *testing.T) {
	svc, db := setupService(t)
	ctx := context.Background()

	// wallet_1 has 10000. Attempt 20 concurrent transfers of 1000 each.
	// Only 10 should succeed — no double spending.
	numTransfers := 20
	var wg sync.WaitGroup
	results := make([]*service.TransferResult, numTransfers)
	errs := make([]error, numTransfers)

	for i := 0; i < numTransfers; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := service.TransferRequest{
				IdempotencyKey: fmt.Sprintf("concurrent-%d", idx),
				FromWalletID:   "wallet_1",
				ToWalletID:     "wallet_2",
				Amount:         1000,
			}
			results[idx], errs[idx] = svc.CreateTransfer(ctx, req)
		}(i)
	}
	wg.Wait()

	// Count successes and failures
	var processed, failed int
	for i := 0; i < numTransfers; i++ {
		require.NoError(t, errs[i], "transfer %d should not have an error", i)
		require.NotNil(t, results[i])
		switch results[i].Transfer.Status {
		case domain.TransferStatusProcessed:
			processed++
		case domain.TransferStatusFailed:
			failed++
		}
	}

	assert.Equal(t, 10, processed, "exactly 10 transfers should succeed")
	assert.Equal(t, 10, failed, "exactly 10 transfers should fail")

	// Verify final balance — wallet_1 should be fully drained
	var balance1 int64
	err := db.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = 'wallet_1'`).Scan(&balance1)
	require.NoError(t, err)
	assert.Equal(t, int64(0), balance1)

	// wallet_2 should have received exactly 10000
	var balance2 int64
	err = db.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = 'wallet_2'`).Scan(&balance2)
	require.NoError(t, err)
	assert.Equal(t, int64(15000), balance2) // 5000 + 10000
}
