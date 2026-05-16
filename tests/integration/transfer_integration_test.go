package integration

// import (
// 	"context"
// 	"database/sql"
// 	"os"
// 	"strconv"
// 	"sync"
// 	"testing"

// 	"wallet-transfer/internal/database"
// 	"wallet-transfer/internal/idempotency"
// 	ledgerrepo "wallet-transfer/internal/ledger/repository"
// 	"wallet-transfer/internal/transfer/dto"
// 	transferrepo "wallet-transfer/internal/transfer/repository"
// 	transferservice "wallet-transfer/internal/transfer/service"
// 	walletdomain "wallet-transfer/internal/wallet/domain"
// 	walletrepo "wallet-transfer/internal/wallet/repository"
// 	"wallet-transfer/pkg/logger"

// 	"github.com/google/uuid"
// 	"github.com/stretchr/testify/assert"
// )

// func newIntegrationDB(t *testing.T) *database.Connection {
// 	t.Helper()

// 	dbConfig := database.Config{
// 		Host:     getEnv("DB_HOST", "localhost"),
// 		Port:     getEnvInt("DB_PORT", 5432),
// 		User:     getEnv("DB_USER", "postgres"),
// 		Password: getEnv("DB_PASSWORD", "postgres"),
// 		DBName:   getEnv("DB_NAME", "testdb"),
// 		SSLMode:  getEnv("DB_SSLMODE", "disable"),
// 	}

// 	db, err := database.New(dbConfig)
// 	if err != nil {
// 		t.Fatalf("failed to connect to integration database: %v", err)
// 	}

// 	return db
// }

// func getEnv(key, defaultValue string) string {
// 	value := os.Getenv(key)
// 	if value == "" {
// 		return defaultValue
// 	}
// 	return value
// }

// func getEnvInt(key string, defaultValue int) int {
// 	value := os.Getenv(key)
// 	if value == "" {
// 		return defaultValue
// 	}
// 	parsed, err := strconv.Atoi(value)
// 	if err != nil {
// 		return defaultValue
// 	}
// 	return parsed
// }

// func createTransferService(t *testing.T, db *database.Connection) *transferservice.TransferService {
// 	t.Helper()
// 	walletRepo := walletrepo.NewWalletRepository(db.GetDB())
// 	transferRepo := transferrepo.NewTransferRepository(db.GetDB())
// 	ledgerRepo := ledgerrepo.NewLedgerRepository(db.GetDB())
// 	idempotencyRepo := idempotency.NewIdempotencyRepository(db.GetDB())
// 	logger := logger.New(logger.DEBUG)

// 	return transferservice.NewTransferService(db, walletRepo, transferRepo, ledgerRepo, idempotencyRepo, logger)
// }

// func createWallets(t *testing.T, db *database.Connection, fromBalance, toBalance int64) (string, string) {
// 	t.Helper()
// 	walletRepo := walletrepo.NewWalletRepository(db.GetDB())
// 	ctx := context.Background()
// 	tx, err := db.BeginTx(ctx)
// 	if err != nil {
// 		t.Fatalf("failed to begin tx: %v", err)
// 	}
// 	defer func() {
// 		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
// 			t.Fatalf("failed to rollback tx: %v", rbErr)
// 		}
// 	}()

// 	fromWallet := walletdomain.NewWallet(uuid.NewString(), uuid.NewString(), "USD")
// 	fromWallet.Balance = fromBalance
// 	if err := walletRepo.Create(ctx, tx, fromWallet); err != nil {
// 		t.Fatalf("failed to create from wallet: %v", err)
// 	}

// 	toWallet := walletdomain.NewWallet(uuid.NewString(), uuid.NewString(), "USD")
// 	toWallet.Balance = toBalance
// 	if err := walletRepo.Create(ctx, tx, toWallet); err != nil {
// 		t.Fatalf("failed to create to wallet: %v", err)
// 	}

// 	if err := tx.Commit(); err != nil {
// 		t.Fatalf("failed to commit wallet creation tx: %v", err)
// 	}

// 	return fromWallet.ID, toWallet.ID
// }

// func TestTransferCreatesBalancedLedgerAndUpdatesBalances(t *testing.T) {
// 	db := newIntegrationDB(t)
// 	defer db.Close()

// 	fromWalletID, toWalletID := createWallets(t, db, 10000, 2000)
// 	transferSvc := createTransferService(t, db)
// 	ledgerRepo := ledgerrepo.NewLedgerRepository(db.GetDB())
// 	walletRepo := walletrepo.NewWalletRepository(db.GetDB())

// 	ctx := context.Background()
// 	req := dto.CreateTransferRequest{
// 		IdempotencyKey: uuid.NewString(),
// 		FromWalletID:   fromWalletID,
// 		ToWalletID:     toWalletID,
// 		Amount:         5000,
// 	}

// 	resp, err := transferSvc.CreateTransfer(ctx, req)
// 	assert.NoError(t, err)
// 	assert.Equal(t, fromWalletID, resp.FromWalletID)
// 	assert.Equal(t, toWalletID, resp.ToWalletID)
// 	assert.Equal(t, int64(5000), resp.Amount)
// 	assert.Equal(t, "PROCESSED", resp.Status)

// 	// Verify balances
// 	tx, err := db.BeginTx(ctx)
// 	assert.NoError(t, err)
// 	defer func() {
// 		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
// 			t.Fatalf("failed to rollback tx: %v", rbErr)
// 		}
// 	}()

// 	fromWallet, err := walletRepo.GetByID(ctx, tx, fromWalletID)
// 	assert.NoError(t, err)
// 	assert.NotNil(t, fromWallet)
// 	assert.Equal(t, int64(5000), fromWallet.Balance)

// 	toWallet, err := walletRepo.GetByID(ctx, tx, toWalletID)
// 	assert.NoError(t, err)
// 	assert.NotNil(t, toWallet)
// 	assert.Equal(t, int64(7000), toWallet.Balance)

// 	entries, err := ledgerRepo.GetEntriesByTransfer(ctx, tx, resp.TransferID)
// 	assert.NoError(t, err)
// 	assert.Len(t, entries, 2)

// 	var debit, credit int64
// 	for _, entry := range entries {
// 		if entry.Type == "DEBIT" {
// 			debit += entry.Amount
// 		} else if entry.Type == "CREDIT" {
// 			credit += entry.Amount
// 		}
// 	}
// 	assert.Equal(t, int64(5000), debit)
// 	assert.Equal(t, int64(5000), credit)
// }

// func TestTransferIsIdempotent(t *testing.T) {
// 	db := newIntegrationDB(t)
// 	defer db.Close()

// 	fromWalletID, toWalletID := createWallets(t, db, 15000, 1000)
// 	transferSvc := createTransferService(t, db)
// 	walletRepo := walletrepo.NewWalletRepository(db.GetDB())

// 	ctx := context.Background()
// 	idempotencyKey := uuid.NewString()
// 	req := dto.CreateTransferRequest{
// 		IdempotencyKey: idempotencyKey,
// 		FromWalletID:   fromWalletID,
// 		ToWalletID:     toWalletID,
// 		Amount:         7000,
// 	}

// 	firstResp, err := transferSvc.CreateTransfer(ctx, req)
// 	assert.NoError(t, err)

// 	secondResp, err := transferSvc.CreateTransfer(ctx, req)
// 	assert.NoError(t, err)
// 	assert.Equal(t, firstResp.TransferID, secondResp.TransferID)
// 	assert.Equal(t, firstResp.Status, secondResp.Status)

// 	// Ensure only one transfer and correct balances
// 	tx, err := db.BeginTx(ctx)
// 	assert.NoError(t, err)
// 	defer func() {
// 		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
// 			t.Fatalf("failed to rollback tx: %v", rbErr)
// 		}
// 	}()

// 	fromWallet, err := walletRepo.GetByID(ctx, tx, fromWalletID)
// 	assert.NoError(t, err)
// 	assert.Equal(t, int64(8000), fromWallet.Balance)

// 	toWallet, err := walletRepo.GetByID(ctx, tx, toWalletID)
// 	assert.NoError(t, err)
// 	assert.Equal(t, int64(8000), toWallet.Balance)
// }

// func TestConcurrentTransfersDoNotDoubleSpend(t *testing.T) {
// 	db := newIntegrationDB(t)
// 	defer db.Close()

// 	fromWalletID, toWalletID := createWallets(t, db, 10000, 0)
// 	transferSvc := createTransferService(t, db)
// 	walletRepo := walletrepo.NewWalletRepository(db.GetDB())

// 	ctx := context.Background()
// 	idempotencyKey := uuid.NewString()

// 	req := dto.CreateTransferRequest{
// 		IdempotencyKey: idempotencyKey,
// 		FromWalletID:   fromWalletID,
// 		ToWalletID:     toWalletID,
// 		Amount:         10000,
// 	}

// 	var wg sync.WaitGroup
// 	var mu sync.Mutex
// 	var transferIDs []string
// 	var errorsCount int

// 	for i := 0; i < 5; i++ {
// 		wg.Add(1)
// 		go func() {
// 			defer wg.Done()
// 			resp, err := transferSvc.CreateTransfer(ctx, req)
// 			mu.Lock()
// 			if err != nil {
// 				errorsCount++
// 			} else {
// 				transferIDs = append(transferIDs, resp.TransferID)
// 			}
// 			mu.Unlock()
// 		}()
// 	}

// 	wg.Wait()

// 	assert.Len(t, transferIDs, 5)
// 	for _, id := range transferIDs {
// 		assert.Equal(t, transferIDs[0], id)
// 	}
// 	assert.Equal(t, 0, errorsCount)

// 	tx, err := db.BeginTx(ctx)
// 	assert.NoError(t, err)
// 	defer func() {
// 		if rbErr := tx.Rollback(); rbErr != nil && rbErr != sql.ErrTxDone {
// 			t.Fatalf("failed to rollback tx: %v", rbErr)
// 		}
// 	}()

// 	fromWallet, err := walletRepo.GetByID(ctx, tx, fromWalletID)
// 	assert.NoError(t, err)
// 	assert.Equal(t, int64(0), fromWallet.Balance)

// 	toWallet, err := walletRepo.GetByID(ctx, tx, toWalletID)
// 	assert.NoError(t, err)
// 	assert.Equal(t, int64(10000), toWallet.Balance)
// }
