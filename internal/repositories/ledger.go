package repositories

import (
	"context"

	"github.com/manish8561/wallet-transfer-assignment/internal/models"
	"gorm.io/gorm"
)

// LedgerRepository handles ledger entry persistence
type LedgerRepository struct {
	db *gorm.DB
}

// NewLedgerRepository creates a new ledger repository
func NewLedgerRepository(db *gorm.DB) *LedgerRepository {
	return &LedgerRepository{db: db}
}

// Create creates a new ledger entry
func (r *LedgerRepository) Create(ctx context.Context, tx *gorm.DB, walletID, transferID, entryType string, amount int64, entryID string) (*models.LedgerEntry, error) {
	entry := &models.LedgerEntry{
		EntryID:    entryID,
		WalletID:   walletID,
		TransferID: transferID,
		Type:       entryType,
		Amount:     amount,
	}
	err := tx.WithContext(ctx).Create(entry).Error
	return entry, err
}

// GetByTransfer retrieves ledger entries for a transfer
func (r *LedgerRepository) GetByTransfer(ctx context.Context, transferID string) ([]*models.LedgerEntry, error) {
	var entries []*models.LedgerEntry
	err := r.db.WithContext(ctx).
		Where("transfer_id = ?", transferID).
		Order("created_at ASC").
		Find(&entries).Error
	return entries, err
}

// VerifyBalance verifies that ledger entries for a transfer are balanced
func (r *LedgerRepository) VerifyBalance(ctx context.Context, transferID string) (bool, error) {
	var debitSum, creditSum int64

	// Get debit sum
	err := r.db.WithContext(ctx).
		Model(&models.LedgerEntry{}).
		Where("transfer_id = ? AND type = ?", transferID, models.EntryTypeDebit).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&debitSum).Error
	if err != nil {
		return false, err
	}

	// Get credit sum
	err = r.db.WithContext(ctx).
		Model(&models.LedgerEntry{}).
		Where("transfer_id = ? AND type = ?", transferID, models.EntryTypeCredit).
		Select("COALESCE(SUM(amount), 0)").
		Scan(&creditSum).Error
	if err != nil {
		return false, err
	}

	return debitSum == creditSum, nil
}

// GetLedgerBalance retrieves wallet balance from ledger
func (r *LedgerRepository) GetLedgerBalance(ctx context.Context, walletID string) (int64, error) {
	var balance int64
	err := r.db.WithContext(ctx).
		Model(&models.LedgerEntry{}).
		Where("wallet_id = ?", walletID).
		Select("COALESCE(SUM(CASE WHEN type = ? THEN amount ELSE -amount END), 0)", models.EntryTypeCredit).
		Scan(&balance).Error
	return balance, err
}
