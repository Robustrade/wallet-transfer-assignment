package postgres

import (
	"context"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"gorm.io/gorm"
)

type ledgerRepo struct {
	db *gorm.DB
}

// NewLedgerRepository creates a new GORM-backed LedgerRepository.
func NewLedgerRepository(db *gorm.DB) repository.LedgerRepository {
	return &ledgerRepo{db: db}
}

func (r *ledgerRepo) CreateEntries(ctx context.Context, tx *gorm.DB, entries []*domain.LedgerEntry) error {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}
	return dbConn.WithContext(ctx).Create(&entries).Error
}

func (r *ledgerRepo) GetByTransferID(ctx context.Context, transferID string) ([]*domain.LedgerEntry, error) {
	var entries []*domain.LedgerEntry
	err := r.db.WithContext(ctx).
		Where("transfer_id = ?", transferID).
		Order("created_at asc").
		Find(&entries).Error
	return entries, err
}

func (r *ledgerRepo) GetByWalletID(ctx context.Context, walletID string) ([]*domain.LedgerEntry, error) {
	var entries []*domain.LedgerEntry
	err := r.db.WithContext(ctx).
		Where("wallet_id = ?", walletID).
		Order("created_at asc").
		Find(&entries).Error
	return entries, err
}

// GetCalculatedBalance computes the sum of credits minus debits from ledger records.
func (r *ledgerRepo) GetCalculatedBalance(ctx context.Context, walletID string) (int64, error) {
	var total int64
	query := `
		SELECT COALESCE(SUM(
			CASE 
				WHEN type = 'CREDIT' THEN amount 
				WHEN type = 'DEBIT' THEN -amount 
				ELSE 0 
			END
		), 0)
		FROM ledger_entries
		WHERE wallet_id = ?
	`
	err := r.db.WithContext(ctx).Raw(query, walletID).Scan(&total).Error
	if err != nil {
		return 0, err
	}
	return total, nil
}
