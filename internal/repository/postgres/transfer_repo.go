package postgres

import (
	"context"
	"errors"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"gorm.io/gorm"
)

type transferRepo struct {
	db *gorm.DB
}

// NewTransferRepository creates a new GORM-backed TransferRepository.
func NewTransferRepository(db *gorm.DB) repository.TransferRepository {
	return &transferRepo{db: db}
}

func (r *transferRepo) Create(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}
	return dbConn.WithContext(ctx).Create(transfer).Error
}

func (r *transferRepo) GetByID(ctx context.Context, id string) (*domain.Transfer, error) {
	var transfer domain.Transfer
	err := r.db.WithContext(ctx).First(&transfer, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	return &transfer, nil
}

func (r *transferRepo) GetByIdempotencyKey(ctx context.Context, key string) (*domain.Transfer, error) {
	var transfer domain.Transfer
	err := r.db.WithContext(ctx).First(&transfer, "idempotency_key = ?", key).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrTransferNotFound
		}
		return nil, err
	}
	return &transfer, nil
}

func (r *transferRepo) Update(ctx context.Context, tx *gorm.DB, transfer *domain.Transfer) error {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}
	return dbConn.WithContext(ctx).Save(transfer).Error
}
