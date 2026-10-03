package service_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"wallet-transfer-assignment/internal/database"
	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository/postgres"
	"wallet-transfer-assignment/internal/service"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// setupTestDB creates an isolated SQLite database per test with full schema auto-migration.
func setupTestDB(t *testing.T) (*gorm.DB, service.TransferService, service.WalletService) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := gorm.Open(sqlite.Open(dbPath+"?_journal_mode=WAL&_busy_timeout=5000"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(10)
	sqlDB.SetMaxIdleConns(10)

	err = database.AutoMigrate(db)
	require.NoError(t, err)

	repo := postgres.NewRepository(db)
	transferSvc := service.NewTransferService(repo)
	walletSvc := service.NewWalletService(repo)

	return db, transferSvc, walletSvc
}

func TestTransferService_Success(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	// Create two wallets
	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_alice",
		InitialBalance: 1000,
		Currency:       "USD",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(1000), w1.Balance)

	w2, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_bob",
		InitialBalance: 500,
		Currency:       "USD",
	})
	require.NoError(t, err)
	assert.Equal(t, int64(500), w2.Balance)

	// Transfer 300 from Alice to Bob
	resp, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "tx-test-001",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         300,
		Currency:       "USD",
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TransferStatusProcessed, resp.Status)
	assert.Equal(t, int64(300), resp.Amount)
	assert.False(t, resp.IsReplay)

	// Verify updated wallet balances
	bal1, err := walletSvc.GetBalance(ctx, "w_alice")
	require.NoError(t, err)
	assert.Equal(t, int64(700), bal1.StoredBalance)
	assert.Equal(t, int64(700), bal1.LedgerBalance)
	assert.True(t, bal1.IsAuditConsistent)

	bal2, err := walletSvc.GetBalance(ctx, "w_bob")
	require.NoError(t, err)
	assert.Equal(t, int64(800), bal2.StoredBalance)
	assert.Equal(t, int64(800), bal2.LedgerBalance)
	assert.True(t, bal2.IsAuditConsistent)

	// Verify ledger entries (exactly 2 entries)
	entries, err := transferSvc.GetTransferLedger(ctx, resp.TransferID)
	require.NoError(t, err)
	require.Len(t, entries, 2)

	assert.Equal(t, "w_alice", entries[0].WalletID)
	assert.Equal(t, domain.LedgerEntryTypeDebit, entries[0].Type)
	assert.Equal(t, int64(300), entries[0].Amount)
	assert.Equal(t, int64(700), entries[0].BalanceAfter)

	assert.Equal(t, "w_bob", entries[1].WalletID)
	assert.Equal(t, domain.LedgerEntryTypeCredit, entries[1].Type)
	assert.Equal(t, int64(300), entries[1].Amount)
	assert.Equal(t, int64(800), entries[1].BalanceAfter)
}

func TestTransferService_IdempotencyReplay(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_alice",
		InitialBalance: 1000,
	})
	require.NoError(t, err)

	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_bob",
		InitialBalance: 200,
	})
	require.NoError(t, err)

	req := service.CreateTransferRequest{
		IdempotencyKey: "replay-key-123",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         400,
	}

	// First execution
	resp1, err := transferSvc.ExecuteTransfer(ctx, req)
	require.NoError(t, err)
	assert.False(t, resp1.IsReplay)
	assert.Equal(t, domain.TransferStatusProcessed, resp1.Status)

	// Second execution (replay)
	resp2, err := transferSvc.ExecuteTransfer(ctx, req)
	require.NoError(t, err)
	assert.True(t, resp2.IsReplay)
	assert.Equal(t, resp1.TransferID, resp2.TransferID)
	assert.Equal(t, resp1.Amount, resp2.Amount)

	// Ensure balance was NOT deducted twice
	bal1, err := walletSvc.GetBalance(ctx, "w_alice")
	require.NoError(t, err)
	assert.Equal(t, int64(600), bal1.StoredBalance)

	bal2, err := walletSvc.GetBalance(ctx, "w_bob")
	require.NoError(t, err)
	assert.Equal(t, int64(600), bal2.StoredBalance)

	// Ensure only 2 ledger entries exist for the transfer
	entries, err := transferSvc.GetTransferLedger(ctx, resp1.TransferID)
	require.NoError(t, err)
	assert.Len(t, entries, 2)
}

func TestTransferService_IdempotencyPayloadMismatch(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	_, _ = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_alice", InitialBalance: 1000})
	_, _ = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_bob", InitialBalance: 200})

	// First call with amount 100
	_, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "conflict-key-1",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         100,
	})
	require.NoError(t, err)

	// Second call with same key but amount 500
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "conflict-key-1",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         500,
	})
	require.ErrorIs(t, err, domain.ErrIdempotencyPayloadMismatch)
}

func TestTransferService_InsufficientBalance(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	_, _ = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_alice", InitialBalance: 100})
	_, _ = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_bob", InitialBalance: 200})

	// Attempt transfer exceeding balance
	resp, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "insufficient-funds-key",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         500,
	})
	require.ErrorIs(t, err, domain.ErrInsufficientBalance)
	require.NotNil(t, resp)
	assert.Equal(t, domain.TransferStatusFailed, resp.Status)
	assert.Equal(t, domain.ErrInsufficientBalance.Error(), resp.FailureReason)

	// Balances remain untouched
	bal1, _ := walletSvc.GetBalance(ctx, "w_alice")
	assert.Equal(t, int64(100), bal1.StoredBalance)
	bal2, _ := walletSvc.GetBalance(ctx, "w_bob")
	assert.Equal(t, int64(200), bal2.StoredBalance)

	// Replay returns the failed transfer response consistently
	replayResp, replayErr := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "insufficient-funds-key",
		FromWalletID:   "w_alice",
		ToWalletID:     "w_bob",
		Amount:         500,
	})
	require.ErrorIs(t, replayErr, domain.ErrInsufficientBalance)
	assert.True(t, replayResp.IsReplay)
	assert.Equal(t, domain.TransferStatusFailed, replayResp.Status)
}

func TestTransferService_ValidationErrors(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, _ := setupTestDB(t)

	// Missing idempotency key
	_, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		FromWalletID: "w_1",
		ToWalletID:   "w_2",
		Amount:       100,
	})
	assert.ErrorIs(t, err, domain.ErrMissingIdempotencyKey)

	// Self-transfer
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "self-transfer-key",
		FromWalletID:   "w_1",
		ToWalletID:     "w_1",
		Amount:         100,
	})
	assert.ErrorIs(t, err, domain.ErrSameWalletTransfer)

	// Non-positive amount
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "neg-amount-key",
		FromWalletID:   "w_1",
		ToWalletID:     "w_2",
		Amount:         -50,
	})
	assert.ErrorIs(t, err, domain.ErrInvalidAmount)
}

func TestTransferService_ConcurrencyNoDoubleSpend(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	// Alice starts with 500
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_alice",
		InitialBalance: 500,
	})
	require.NoError(t, err)

	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_bob",
		InitialBalance: 0,
	})
	require.NoError(t, err)

	// 10 concurrent transfers of 100 each = 1000 total attempted (only 5 can succeed)
	concurrency := 10
	var wg sync.WaitGroup
	var successCount int64
	var failCount int64

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("race-key-%d", idx),
				FromWalletID:   "w_alice",
				ToWalletID:     "w_bob",
				Amount:         100,
			})
			if err == nil {
				atomic.AddInt64(&successCount, 1)
			} else {
				assert.ErrorIs(t, err, domain.ErrInsufficientBalance)
				atomic.AddInt64(&failCount, 1)
			}
		}(i)
	}

	wg.Wait()

	assert.Equal(t, int64(5), successCount, "Exactly 5 transfers should succeed")
	assert.Equal(t, int64(5), failCount, "Exactly 5 transfers should fail due to insufficient funds")

	balAlice, err := walletSvc.GetBalance(ctx, "w_alice")
	require.NoError(t, err)
	assert.Equal(t, int64(0), balAlice.StoredBalance)
	assert.Equal(t, int64(0), balAlice.LedgerBalance)
	assert.True(t, balAlice.IsAuditConsistent)

	balBob, err := walletSvc.GetBalance(ctx, "w_bob")
	require.NoError(t, err)
	assert.Equal(t, int64(500), balBob.StoredBalance)
	assert.Equal(t, int64(500), balBob.LedgerBalance)
	assert.True(t, balBob.IsAuditConsistent)
}

func TestTransferService_ConcurrencyCrossTransfersNoDeadlock(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_a", InitialBalance: 1000})
	require.NoError(t, err)

	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_b", InitialBalance: 1000})
	require.NoError(t, err)

	// Launch concurrent opposing transfers: A->B and B->A simultaneously
	totalOps := 20
	var wg sync.WaitGroup

	for i := 0; i < totalOps; i++ {
		wg.Add(2)

		// A -> B
		go func(idx int) {
			defer wg.Done()
			_, _ = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("a-to-b-%d", idx),
				FromWalletID:   "w_a",
				ToWalletID:     "w_b",
				Amount:         10,
			})
		}(i)

		// B -> A
		go func(idx int) {
			defer wg.Done()
			_, _ = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
				IdempotencyKey: fmt.Sprintf("b-to-a-%d", idx),
				FromWalletID:   "w_b",
				ToWalletID:     "w_a",
				Amount:         10,
			})
		}(i)
	}

	wg.Wait()

	// Both balances must remain 1000 and consistent with ledger
	balA, err := walletSvc.GetBalance(ctx, "w_a")
	require.NoError(t, err)
	assert.Equal(t, int64(1000), balA.StoredBalance)
	assert.True(t, balA.IsAuditConsistent)

	balB, err := walletSvc.GetBalance(ctx, "w_b")
	require.NoError(t, err)
	assert.Equal(t, int64(1000), balB.StoredBalance)
	assert.True(t, balB.IsAuditConsistent)
}

func TestTransferService_CurrencyMismatch(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	// Create USD wallet and EUR wallet
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_usd",
		InitialBalance: 1000,
		Currency:       "USD",
	})
	require.NoError(t, err)

	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_eur",
		InitialBalance: 1000,
		Currency:       "EUR",
	})
	require.NoError(t, err)

	// 1. Cross-wallet currency mismatch (USD -> EUR)
	resp, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "cross-curr-key-1",
		FromWalletID:   "w_usd",
		ToWalletID:     "w_eur",
		Amount:         100,
		Currency:       "USD",
	})
	require.ErrorIs(t, err, domain.ErrCurrencyMismatch)
	require.NotNil(t, resp)
	assert.Equal(t, domain.TransferStatusFailed, resp.Status)
	assert.Equal(t, domain.ErrCurrencyMismatch.Error(), resp.FailureReason)

	// Balances remain untouched
	balUSD, _ := walletSvc.GetBalance(ctx, "w_usd")
	assert.Equal(t, int64(1000), balUSD.StoredBalance)
	balEUR, _ := walletSvc.GetBalance(ctx, "w_eur")
	assert.Equal(t, int64(1000), balEUR.StoredBalance)

	// 2. Request currency does not match wallet currencies
	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_usd2",
		InitialBalance: 500,
		Currency:       "USD",
	})
	require.NoError(t, err)

	resp2, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "req-curr-mismatch-key",
		FromWalletID:   "w_usd",
		ToWalletID:     "w_usd2",
		Amount:         100,
		Currency:       "EUR", // Request asks for EUR on USD wallets
	})
	require.ErrorIs(t, err, domain.ErrCurrencyMismatch)
	require.NotNil(t, resp2)
	assert.Equal(t, domain.TransferStatusFailed, resp2.Status)

	// 3. Replay returns the failed currency mismatch response consistently
	replayResp, replayErr := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "cross-curr-key-1",
		FromWalletID:   "w_usd",
		ToWalletID:     "w_eur",
		Amount:         100,
		Currency:       "USD",
	})
	require.ErrorIs(t, replayErr, domain.ErrCurrencyMismatch)
	assert.True(t, replayResp.IsReplay)
	assert.Equal(t, domain.TransferStatusFailed, replayResp.Status)
}

func TestTransferService_MatchingNonUSDCurrency(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_eur1",
		InitialBalance: 1000,
		Currency:       "EUR",
	})
	require.NoError(t, err)

	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_eur2",
		InitialBalance: 500,
		Currency:       "EUR",
	})
	require.NoError(t, err)

	// Transfer without explicit currency inherits EUR from wallets
	resp, err := transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "eur-transfer-key",
		FromWalletID:   "w_eur1",
		ToWalletID:     "w_eur2",
		Amount:         300,
	})
	require.NoError(t, err)
	assert.Equal(t, domain.TransferStatusProcessed, resp.Status)
	assert.Equal(t, "EUR", resp.Currency)

	bal1, _ := walletSvc.GetBalance(ctx, "w_eur1")
	assert.Equal(t, int64(700), bal1.StoredBalance)
	bal2, _ := walletSvc.GetBalance(ctx, "w_eur2")
	assert.Equal(t, int64(800), bal2.StoredBalance)
}

func TestTransferService_NotFoundWalletIdempotency(t *testing.T) {
	ctx := context.Background()
	_, transferSvc, walletSvc := setupTestDB(t)

	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_valid",
		InitialBalance: 1000,
	})
	require.NoError(t, err)

	req := service.CreateTransferRequest{
		IdempotencyKey: "not-found-idem-key",
		FromWalletID:   "w_nonexistent",
		ToWalletID:     "w_valid",
		Amount:         100,
	}

	// First execution fails with ErrSourceWalletNotFound
	resp1, err1 := transferSvc.ExecuteTransfer(ctx, req)
	require.ErrorIs(t, err1, domain.ErrSourceWalletNotFound)
	require.NotNil(t, resp1)
	assert.Equal(t, domain.TransferStatusFailed, resp1.Status)
	assert.False(t, resp1.IsReplay)

	// Replay returns the exact same failed result idempotently
	resp2, err2 := transferSvc.ExecuteTransfer(ctx, req)
	require.ErrorIs(t, err2, domain.ErrSourceWalletNotFound)
	require.NotNil(t, resp2)
	assert.Equal(t, domain.TransferStatusFailed, resp2.Status)
	assert.True(t, resp2.IsReplay)
	assert.Equal(t, resp1.TransferID, resp2.TransferID)
}

func TestTransferService_InactiveWalletIdempotency(t *testing.T) {
	ctx := context.Background()
	db, transferSvc, walletSvc := setupTestDB(t)

	w1, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_active",
		InitialBalance: 1000,
	})
	require.NoError(t, err)

	w2, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{
		ID:             "w_frozen",
		InitialBalance: 500,
	})
	require.NoError(t, err)

	// Freeze wallet 2
	err = db.Model(&domain.Wallet{}).Where("id = ?", w2.ID).Update("status", domain.WalletStatusFrozen).Error
	require.NoError(t, err)

	req := service.CreateTransferRequest{
		IdempotencyKey: "inactive-idem-key",
		FromWalletID:   w1.ID,
		ToWalletID:     w2.ID,
		Amount:         100,
	}

	// First execution fails with ErrWalletInactive
	resp1, err1 := transferSvc.ExecuteTransfer(ctx, req)
	require.ErrorIs(t, err1, domain.ErrWalletInactive)
	require.NotNil(t, resp1)
	assert.Equal(t, domain.TransferStatusFailed, resp1.Status)
	assert.False(t, resp1.IsReplay)

	// Replay returns the exact same failed result idempotently
	resp2, err2 := transferSvc.ExecuteTransfer(ctx, req)
	require.ErrorIs(t, err2, domain.ErrWalletInactive)
	require.NotNil(t, resp2)
	assert.Equal(t, domain.TransferStatusFailed, resp2.Status)
	assert.True(t, resp2.IsReplay)
	assert.Equal(t, resp1.TransferID, resp2.TransferID)
}

func TestLedger_SystemWideDoubleEntryBalance(t *testing.T) {
	ctx := context.Background()
	db, transferSvc, walletSvc := setupTestDB(t)

	// Create 3 wallets with opening balances
	_, err := walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_1", InitialBalance: 1000})
	require.NoError(t, err)
	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_2", InitialBalance: 500})
	require.NoError(t, err)
	_, err = walletSvc.CreateWallet(ctx, service.CreateWalletRequest{ID: "w_3", InitialBalance: 200})
	require.NoError(t, err)

	// Before any transfers, zero ledger entries should exist (opening balances are outside the transfer ledger)
	var initialCount int64
	err = db.Model(&domain.LedgerEntry{}).Count(&initialCount).Error
	require.NoError(t, err)
	assert.Equal(t, int64(0), initialCount, "Transfer ledger should be empty before transfers")

	// Execute several transfers
	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "t1", FromWalletID: "w_1", ToWalletID: "w_2", Amount: 300,
	})
	require.NoError(t, err)

	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "t2", FromWalletID: "w_2", ToWalletID: "w_3", Amount: 150,
	})
	require.NoError(t, err)

	_, err = transferSvc.ExecuteTransfer(ctx, service.CreateTransferRequest{
		IdempotencyKey: "t3", FromWalletID: "w_3", ToWalletID: "w_1", Amount: 50,
	})
	require.NoError(t, err)

	// Fetch all ledger entries
	var allEntries []domain.LedgerEntry
	err = db.Find(&allEntries).Error
	require.NoError(t, err)

	// Exactly 3 transfers * 2 entries = 6 entries total
	assert.Len(t, allEntries, 6)

	// Calculate system-wide debits and credits
	var totalDebits int64
	var totalCredits int64
	entriesByTransfer := make(map[string][]domain.LedgerEntry)

	for _, entry := range allEntries {
		entriesByTransfer[entry.TransferID] = append(entriesByTransfer[entry.TransferID], entry)
		if entry.Type == domain.LedgerEntryTypeDebit {
			totalDebits += entry.Amount
		} else if entry.Type == domain.LedgerEntryTypeCredit {
			totalCredits += entry.Amount
		}
	}

	// 1. System-wide ledger MUST perfectly balance to zero
	assert.Equal(t, totalDebits, totalCredits, "Total debits must equal total credits system-wide")

	// 2. Every transfer MUST have exactly two balanced entries
	for transferID, entries := range entriesByTransfer {
		assert.Len(t, entries, 2, "Transfer %s must have exactly 2 entries", transferID)
		assert.True(t, (entries[0].Type == domain.LedgerEntryTypeDebit && entries[1].Type == domain.LedgerEntryTypeCredit) ||
			(entries[0].Type == domain.LedgerEntryTypeCredit && entries[1].Type == domain.LedgerEntryTypeDebit))
		assert.Equal(t, entries[0].Amount, entries[1].Amount)
	}

	// 3. All wallets must be audit-consistent
	bal1, _ := walletSvc.GetBalance(ctx, "w_1")
	assert.True(t, bal1.IsAuditConsistent)
	assert.Equal(t, int64(750), bal1.StoredBalance) // 1000 - 300 + 50

	bal2, _ := walletSvc.GetBalance(ctx, "w_2")
	assert.True(t, bal2.IsAuditConsistent)
	assert.Equal(t, int64(650), bal2.StoredBalance) // 500 + 300 - 150

	bal3, _ := walletSvc.GetBalance(ctx, "w_3")
	assert.True(t, bal3.IsAuditConsistent)
	assert.Equal(t, int64(300), bal3.StoredBalance) // 200 + 150 - 50
}
