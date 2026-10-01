package repository

import (
	"github.com/google/uuid"
	"github.com/rajdeep-multipl/wallet-transfer-assignment/internal/models"
	"github.com/shopspring/decimal"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WalletRepository struct{}

func NewWalletRepository() *WalletRepository { return &WalletRepository{} }

// Create inserts a wallet. Used by tests and seeding; the assignment does not
// require a wallet-creation endpoint.
func (r *WalletRepository) Create(db *gorm.DB, w *models.Wallet) error {
	return db.Create(w).Error
}

// LockPair locks both wallet rows and returns them as (from, to).
//
// The ORDER BY id happens in SQL, so rows are locked in one fixed order
// regardless of transfer direction. This is the deadlock-avoidance rule from
// ADR section 10. There is deliberately no sorting in Go.
//
// The caller must have already rejected fromID == toID (ErrSameWallet);
// otherwise only one row comes back and this returns ErrWalletNotFound.
//
// Must be called with a transaction handle, or the lock is released
// immediately.
func (r *WalletRepository) LockPair(tx *gorm.DB, fromID, toID uuid.UUID) (from, to *models.Wallet, err error) {
	var wallets []models.Wallet
	err = tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", []uuid.UUID{fromID, toID}).
		Order("id").
		Find(&wallets).Error
	if err != nil {
		return nil, nil, err
	}

	for i := range wallets {
		switch wallets[i].ID {
		case fromID:
			from = &wallets[i]
		case toID:
			to = &wallets[i]
		}
	}
	// Find does not error on missing rows, so check explicitly.
	if from == nil || to == nil {
		return nil, nil, models.ErrWalletNotFound
	}
	return from, to, nil
}

func (r *WalletRepository) Debit(tx *gorm.DB, id uuid.UUID, amount decimal.Decimal) error {
	if !amount.IsPositive() {
		return models.ErrInvalidAmount
	}
	return r.adjustBalance(tx, id, amount.Neg())
}

// Credit adds amount to the wallet's balance. amount must be positive.
func (r *WalletRepository) Credit(tx *gorm.DB, id uuid.UUID, amount decimal.Decimal) error {
	if !amount.IsPositive() {
		return models.ErrInvalidAmount
	}
	return r.adjustBalance(tx, id, amount)
}

// adjustBalance applies a signed delta using a SQL expression. Never use
// Updates(struct) for balances: GORM skips zero-value fields, so a wallet
// debited to exactly 0 would silently not update.
func (r *WalletRepository) adjustBalance(tx *gorm.DB, id uuid.UUID, delta decimal.Decimal) error {
	res := tx.Model(&models.Wallet{}).
		Where("id = ?", id).
		Update("balance", gorm.Expr("balance + ?", delta))
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected != 1 {
		return models.ErrWalletNotFound
	}
	return nil
}
