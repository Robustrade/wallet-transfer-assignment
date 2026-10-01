package repository

import (
	"github.com/google/uuid"
	"gorm.io/gorm"

	"github.com/rajdeep-multipl/wallet-transfer-assignment/internal/models"
)

type LedgerRepository struct{}

func NewLedgerRepository() *LedgerRepository { return &LedgerRepository{} }

func (r *LedgerRepository) InsertPair(tx *gorm.DB, debit, credit *models.LedgerEntry) error {
	return tx.Create([]*models.LedgerEntry{debit, credit}).Error
}

func (r *LedgerRepository) ListByWallet(db *gorm.DB, walletID uuid.UUID) ([]models.LedgerEntry, error) {
	var entries []models.LedgerEntry
	err := db.Where("wallet_id = ?", walletID).
		Order("created_at, id").
		Find(&entries).Error
	return entries, err
}
