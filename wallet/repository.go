package wallet

import (
	"context"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WalletRepo struct{}

func (r *WalletRepo) LockWallets(ctx context.Context, tx *gorm.DB, wallet1 string, wallet2 string) ([]Wallet, error) {

	ids := []string{wallet1, wallet2}
	sort.Strings(ids)

	lockedWallets := make([]Wallet, 2)

	for _, id := range ids {
		var wallet Wallet
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("wallet_id = ?", id).
			Take(&wallet).Error
		if err != nil {
			return nil, err
		}
		if id == wallet1 {
			lockedWallets[0] = wallet
		} else {
			lockedWallets[1] = wallet
		}
	}

	return lockedWallets, nil
}

func (r *WalletRepo) UpdateWallet(ctx context.Context, tx *gorm.DB, wallet_id string, amount float64) error {
	query := `
		UPDATE wallets
		SET balance = $1,
		updated_at = NOW()
		WHERE wallet_id = $2
	`
	err := tx.Exec(
		query,
		amount,
		wallet_id,
	).Error
	if err != nil {
		return err
	}
	return err
}
