package postgres

import (
	"context"
	"errors"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type walletRepo struct {
	db *gorm.DB
}

// NewWalletRepository creates a new GORM-backed WalletRepository.
func NewWalletRepository(db *gorm.DB) repository.WalletRepository {
	return &walletRepo{db: db}
}

func (r *walletRepo) Create(ctx context.Context, tx *gorm.DB, wallet *domain.Wallet) error {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}
	return dbConn.WithContext(ctx).Create(wallet).Error
}

func (r *walletRepo) GetByID(ctx context.Context, id string) (*domain.Wallet, error) {
	var wallet domain.Wallet
	err := r.db.WithContext(ctx).First(&wallet, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return &wallet, nil
}

func (r *walletRepo) GetForUpdate(ctx context.Context, tx *gorm.DB, id string) (*domain.Wallet, error) {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}

	var wallet domain.Wallet
	err := dbConn.WithContext(ctx).
		Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&wallet, "id = ?", id).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, err
	}
	return &wallet, nil
}

// GetPairForUpdate locks two wallets in a strictly deterministic order (lexicographically by ID).
// This guarantees that concurrent transfers in opposite directions (A->B and B->A) never deadlock.
func (r *walletRepo) GetPairForUpdate(ctx context.Context, tx *gorm.DB, fromID, toID string) (*domain.Wallet, *domain.Wallet, error) {
	if fromID == toID {
		return nil, nil, domain.ErrSameWalletTransfer
	}

	firstID, secondID := fromID, toID
	if firstID > secondID {
		firstID, secondID = toID, fromID
	}

	w1, err := r.GetForUpdate(ctx, tx, firstID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			if firstID == fromID {
				return nil, nil, domain.ErrSourceWalletNotFound
			}
			return nil, nil, domain.ErrDestinationWalletNotFound
		}
		return nil, nil, err
	}

	w2, err := r.GetForUpdate(ctx, tx, secondID)
	if err != nil {
		if errors.Is(err, domain.ErrWalletNotFound) {
			if secondID == fromID {
				return nil, nil, domain.ErrSourceWalletNotFound
			}
			return nil, nil, domain.ErrDestinationWalletNotFound
		}
		return nil, nil, err
	}

	if fromID == firstID {
		return w1, w2, nil
	}
	return w2, w1, nil
}

func (r *walletRepo) UpdateBalance(ctx context.Context, tx *gorm.DB, wallet *domain.Wallet) error {
	dbConn := r.db
	if tx != nil {
		dbConn = tx
	}
	return dbConn.WithContext(ctx).
		Model(&domain.Wallet{}).
		Where("id = ?", wallet.ID).
		Updates(map[string]interface{}{
			"balance":    wallet.Balance,
			"updated_at": wallet.UpdatedAt,
		}).Error
}
