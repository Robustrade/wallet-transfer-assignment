package wallet

import (
	"context"
	"sort"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type WalletRepo struct{}

func (r *WalletRepo) LockWalletsAndUpdate(ctx context.Context, tx *gorm.DB, wallet1 string, wallet2 string, amount float64) ([]Wallet, error) {

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
	// var fromWalletIdx, toWalletIdx int
	// if lockedWallets[0].WalletID == wallet1 {
	// 	fromWalletIdx = 0
	// 	toWalletIdx = 1
	// } else {
	// 	fromWalletIdx = 1
	// 	toWalletIdx = 0
	// }

	// if lockedWallets[fromWalletIdx].Balance < amount {
	// 	fmt.Println("insufficient balaance.")
	// 	return errors.New("insufficient balance")
	// }

	// lockedWallets[fromWalletIdx].Balance -= amount
	// lockedWallets[toWalletIdx].Balance += amount

	// query := `
	// 	UPDATE wallets
	// 	SET balance = $1,
	// 	updated_at = NOW()
	// 	WHERE wallet_id = $2
	// `
	// for i := 0; i < 2; i++ {
	// 	err := tx.Exec(
	// 		query,
	// 		lockedWallets[i].Balance,
	// 		lockedWallets[i].WalletID,
	// 	).Error
	// 	if err != nil {
	// 		return err
	// 	}
	// }

	// return nil
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
