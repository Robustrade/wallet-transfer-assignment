package tests

import (
	"context"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"
	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

// TestHelper provides test utilities
type TestHelper struct {
	DB  *gorm.DB
	ctx context.Context
}

// SetupTestDB initializes a test database with PostgreSQL container
func SetupTestDB(ctx context.Context) (*TestHelper, error) {
	// Optional load for local development; env vars may be passed directly in CI/scripts
	_ = godotenv.Load()

	host := os.Getenv("DB_HOST")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	port := os.Getenv("DB_PORT")
	dbName := os.Getenv("DB_NAME")

	// Connect to database using GORM
	dsn := "host=" + host + " port=" + port + " user=" + user + " password=" + password + " dbname=" + dbName + " sslmode=disable"
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}

	// Run migrations using GORM AutoMigrate
	if err := db.AutoMigrate(
		&models.Wallet{},
		&models.Transfer{},
		&models.LedgerEntry{},
		&models.IdempotencyRecord{},
	); err != nil {
		return nil, err
	}

	return &TestHelper{
		DB:  db,
		ctx: ctx,
	}, nil
}

// CreateWallet creates a test wallet
func (h *TestHelper) CreateWallet(balance int64) (string, error) {
	walletID := uuid.New().String()
	wallet := models.Wallet{
		WalletID:  walletID,
		Balance:   balance,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.DB.Create(&wallet).Error; err != nil {
		return "", err
	}
	return walletID, nil
}

// GetWalletBalance retrieves wallet balance
func (h *TestHelper) GetWalletBalance(walletID string) (int64, error) {
	var wallet models.Wallet
	err := h.DB.Where("wallet_id = ?", walletID).First(&wallet).Error
	return wallet.Balance, err
}

// GetLedgerEntries retrieves ledger entries for a transfer
func (h *TestHelper) GetLedgerEntries(transferID string) ([]map[string]interface{}, error) {
	var entries []models.LedgerEntry
	err := h.DB.Where("transfer_id = ?", transferID).Order("created_at").Find(&entries).Error
	if err != nil {
		return nil, err
	}

	result := make([]map[string]interface{}, len(entries))
	for i, entry := range entries {
		result[i] = map[string]interface{}{
			"entry_id":  entry.EntryID,
			"wallet_id": entry.WalletID,
			"type":      entry.Type,
			"amount":    entry.Amount,
		}
	}
	return result, nil
}

// GetTransfer retrieves transfer details
func (h *TestHelper) GetTransfer(transferID string) (map[string]interface{}, error) {
	var transfer models.Transfer
	err := h.DB.Where("transfer_id = ?", transferID).First(&transfer).Error
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"transfer_id":    transfer.TransferID,
		"from_wallet_id": transfer.FromWalletID,
		"to_wallet_id":   transfer.ToWalletID,
		"amount":         transfer.Amount,
		"status":         transfer.Status,
	}, nil
}

// CountLedgerEntries counts total ledger entries
func (h *TestHelper) CountLedgerEntries() (int, error) {
	var count int64
	err := h.DB.Model(&models.LedgerEntry{}).Count(&count).Error
	return int(count), err
}

// VerifyLedgerBalance verifies that total debits equal credits
func (h *TestHelper) VerifyLedgerBalance() (bool, error) {
	var result struct {
		Debits  int64
		Credits int64
	}
	err := h.DB.Model(&models.LedgerEntry{}).
		Select(`
			COALESCE(SUM(CASE WHEN type = 'DEBIT' THEN amount ELSE 0 END), 0) as debits,
			COALESCE(SUM(CASE WHEN type = 'CREDIT' THEN amount ELSE 0 END), 0) as credits
		`).Scan(&result).Error
	if err != nil {
		return false, err
	}
	return result.Debits == result.Credits, nil
}

// Cleanup cleans up test resources
func (h *TestHelper) Cleanup() error {
	sqlDB, err := h.DB.DB()
	if err != nil {
		return err
	}
	if sqlDB != nil {
		sqlDB.Close()
	}
	return nil
}

// ClearAllTables clears all tables for test isolation
func (h *TestHelper) ClearAllTables() error {
	return h.DB.Exec(`
		DELETE FROM idempotency_records;
		DELETE FROM ledger_entries;
		DELETE FROM transfers;
		DELETE FROM wallets;
	`).Error
}
