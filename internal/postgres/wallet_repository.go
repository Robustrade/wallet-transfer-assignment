package postgres

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/devthedeveloper/wallet-transfer-assignment/internal/domain"
)

type walletRepository struct {
	q *gorm.DB
}

func (r *walletRepository) Get(ctx context.Context, id string) (domain.Wallet, error) {
	var m walletModel
	if err := r.q.WithContext(ctx).Where("id = ?", id).Take(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Wallet{}, domain.ErrWalletNotFound
		}
		return domain.Wallet{}, translateError(err)
	}
	return walletFromModel(m), nil
}

// GetForUpdate locks the row FOR NO KEY UPDATE, not FOR UPDATE — see
// docs/design.md §7 for why: inserting a transfer row takes an implicit
// FOR KEY SHARE lock on both wallets for the foreign keys, and FOR UPDATE
// conflicts with that in a way that can deadlock even with a sorted
// lock-acquisition order. This lock clause is issued as raw SQL via
// clause.Locking, not left to any GORM default, precisely because it's
// load-bearing for the concurrency guarantees this service makes.
func (r *walletRepository) GetForUpdate(ctx context.Context, id string) (domain.Wallet, error) {
	var m walletModel
	err := r.q.WithContext(ctx).
		Clauses(clause.Locking{Strength: "NO KEY UPDATE"}).
		Where("id = ?", id).
		Take(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.Wallet{}, domain.ErrWalletNotFound
		}
		return domain.Wallet{}, translateError(err)
	}
	return walletFromModel(m), nil
}

func (r *walletRepository) Create(ctx context.Context, w domain.Wallet) error {
	m := walletModel{
		ID:           w.ID,
		Kind:         string(w.Kind),
		Currency:     w.Currency,
		BalanceMinor: w.BalanceMinor,
	}
	if err := r.q.WithContext(ctx).Create(&m).Error; err != nil {
		return translateError(err)
	}
	return nil
}

// ApplyDelta is the second of the three defence layers against negative
// balances described in docs/design.md §7 (the first being the locked
// balance check the service performs before calling this; the third is the
// database CHECK constraint). It is raw SQL, not a GORM Updates() call,
// because the guard clause and the RETURNING value both need to be exact:
// zero rows returned means the guard rejected the update.
func (r *walletRepository) ApplyDelta(ctx context.Context, id string, delta int64) (int64, error) {
	var newBalance int64
	tx := r.q.WithContext(ctx).Raw(`
		UPDATE wallets
		   SET balance_minor = balance_minor + ?, updated_at = now()
		 WHERE id = ?
		   AND (kind = 'SYSTEM' OR balance_minor + ? >= 0)
		RETURNING balance_minor`,
		delta, id, delta,
	).Scan(&newBalance)
	if tx.Error != nil {
		return 0, translateError(tx.Error)
	}
	if tx.RowsAffected == 0 {
		if _, getErr := r.Get(ctx, id); errors.Is(getErr, domain.ErrWalletNotFound) {
			return 0, domain.ErrWalletNotFound
		}
		return 0, domain.ErrInsufficientFunds
	}
	return newBalance, nil
}

func walletFromModel(m walletModel) domain.Wallet {
	return domain.Wallet{
		ID:           m.ID,
		Kind:         domain.WalletKind(m.Kind),
		Currency:     m.Currency,
		BalanceMinor: m.BalanceMinor,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
	}
}
