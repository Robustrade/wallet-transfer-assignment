package repository

import (
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/Robustrade/wallet-transfer-assignment/internal/domain"
)

type ledgerEntryModel struct {
	ID         int64     `gorm:"column:id;primaryKey;autoIncrement"`
	TransferID string    `gorm:"column:transfer_id"`
	WalletID   string    `gorm:"column:wallet_id"`
	EntryType  string    `gorm:"column:entry_type"`
	Amount     int64     `gorm:"column:amount"`
	CreatedAt  time.Time `gorm:"column:created_at"`
}

func (ledgerEntryModel) TableName() string {
	return "ledger_entries"
}

func (postgresRepository) CreateLedgerEntries(db *gorm.DB, entries []domain.LedgerEntry) error {
	now := dbNow()
	models := make([]ledgerEntryModel, len(entries))
	for i, entry := range entries {
		models[i] = ledgerEntryModel{
			TransferID: entry.TransferID,
			WalletID:   entry.WalletID,
			EntryType:  string(entry.Type),
			Amount:     entry.Amount,
			CreatedAt:  now,
		}
	}
	if err := db.Create(&models).Error; err != nil {
		return fmt.Errorf("insert ledger entries: %w", err)
	}
	return nil
}
