package postgres

import (
	"context"

	"github.com/Robustrade/wallet-transfer-assignment/internal/model"
	"github.com/Robustrade/wallet-transfer-assignment/internal/repository"
	"gorm.io/gorm"
)

type ledgerRepository struct {
	db *gorm.DB
}

// NewLedgerRepository returns a Postgres-backed LedgerRepository.
func NewLedgerRepository(db *gorm.DB) repository.LedgerRepository {
	return &ledgerRepository{db: db}
}

func (r *ledgerRepository) CreateEntries(ctx context.Context, tx *gorm.DB, entries []model.LedgerEntry) error {
	if tx == nil {
		tx = r.db.WithContext(ctx)
	}
	return translateError(tx.Create(&entries).Error)
}
