package repository

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/ravindranathreddy/wallet-transfer-assignment/internal/domain"
)

type WalletRepository struct{}

func NewWalletRepository() *WalletRepository {
	return &WalletRepository{}
}

func (r *WalletRepository) GetWalletByID(ctx context.Context, db Executor, walletID string) (domain.Wallet, error) {
	selectQuery := `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = $1`
	var wallet domain.Wallet
	err := db.QueryRow(ctx, selectQuery, walletID).Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.Wallet{}, domain.ErrWalletNotFound
		}
		return domain.Wallet{}, err
	}
	return wallet, nil
}

func (r *WalletRepository) LockForUpdate(ctx context.Context, tx pgx.Tx, walletIDs []string) (map[string]domain.Wallet, error) {
	sorted := append([]string(nil), walletIDs...)
	sort.Strings(sorted)

	result := make(map[string]domain.Wallet, len(sorted))
	for _, id := range sorted {
		row := tx.QueryRow(ctx, `
			SELECT id, balance, created_at, updated_at
			FROM wallets WHERE id = $1
			FOR UPDATE`, id)

		var w domain.Wallet
		if err := row.Scan(&w.ID, &w.Balance, &w.CreatedAt, &w.UpdatedAt); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return nil, domain.ErrWalletNotFound
			}
			return nil, err
		}
		result[w.ID] = w
	}
	return result, nil
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, db Executor, walletID string, newBalance int64) error {
	updateQuery := `UPDATE wallets SET balance = $1, updated_at = NOW() WHERE id = $2`
	_, err := db.Exec(ctx, updateQuery, newBalance, walletID)
	return err
}
