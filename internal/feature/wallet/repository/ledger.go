package repository

import (
	"context"
	"fmt"

	"wallet-transfer-assignment/internal/feature/wallet/domain"
	"wallet-transfer-assignment/internal/feature/wallet/model"

	"gorm.io/gorm"
)

type LedgerRepository interface {
	CreateEntries(
		ctx context.Context,
		tx *gorm.DB,
		entries []domain.LedgerEntry,
	) error

	GetByTransferID(
		ctx context.Context,
		tx *gorm.DB,
		transferID int64,
	) ([]domain.LedgerEntry, error)
}

type ledgerRepository struct{}

func NewLedgerRepository() LedgerRepository {
	return &ledgerRepository{}
}

// CreateEntries inserts both ledger entries in the provided transaction.
func (r *ledgerRepository) CreateEntries(ctx context.Context, tx *gorm.DB, entries []domain.LedgerEntry) error {
	if len(entries) != 2 {
		return domain.ErrInvalidLedgerEntry
	}

	if entries[0].EntryType != domain.LedgerDebit ||
		entries[1].EntryType != domain.LedgerCredit ||
		entries[0].TransferID != entries[1].TransferID ||
		entries[0].WalletID == entries[1].WalletID ||
		entries[0].Amount != entries[1].Amount {
		return domain.ErrInvalidLedgerEntry
	}

	records := make([]model.LedgerEntry, 0, len(entries))

	for _, entry := range entries {
		if err := entry.Validate(); err != nil {
			return err
		}

		records = append(records, model.LedgerEntry{
			TransferID: entry.TransferID,
			WalletID:   entry.WalletID,
			EntryType:  string(entry.EntryType),
			Amount:     entry.Amount,
		})
	}

	if err := tx.WithContext(ctx).Create(&records).Error; err != nil {
		return fmt.Errorf("create ledger entries: %w", err)
	}

	return nil
}

// GetByTransferID returns ledger entries for a transfer.
func (r *ledgerRepository) GetByTransferID(ctx context.Context, tx *gorm.DB, transferID int64) ([]domain.LedgerEntry, error) {
	var records []model.LedgerEntry

	err := tx.WithContext(ctx).
		Where("transfer_id = ?", transferID).
		Order("id ASC").
		Find(&records).Error

	if err != nil {
		return nil, fmt.Errorf("get ledger entries: %w", err)
	}

	entries := make([]domain.LedgerEntry, 0, len(records))

	for _, record := range records {
		entries = append(entries, domain.LedgerEntry{
			ID:         record.ID,
			TransferID: record.TransferID,
			WalletID:   record.WalletID,
			EntryType:  domain.LedgerEntryType(record.EntryType),
			Amount:     record.Amount,
			CreatedAt:  record.CreatedAt,
		})
	}

	return entries, nil
}
