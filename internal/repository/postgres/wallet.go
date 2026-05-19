package postgres

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/avinilcodes/wallet-transfer-assignment/internal/domain"
)

const walletColumns = `wallet_id, user_id, balance, currency, created_at`

// WalletRepository implements repository.WalletRepository.
type WalletRepository struct {
	db *sql.DB
}

// GetByID loads a wallet by primary key.
func (r *WalletRepository) GetByID(ctx context.Context, walletID string) (*domain.Wallet, error) {
	exec := dbOrTx(ctx, r.db)
	query := `SELECT ` + walletColumns + ` FROM wallets WHERE wallet_id = $1`
	row := exec.QueryRowContext(ctx, query, walletID)
	return scanWallet(row)
}

// LockByID loads a wallet with FOR UPDATE inside the current transaction.
func (r *WalletRepository) LockByID(ctx context.Context, walletID string) (*domain.Wallet, error) {
	exec := dbOrTx(ctx, r.db)
	query := `SELECT ` + walletColumns + ` FROM wallets WHERE wallet_id = $1 FOR UPDATE`
	row := exec.QueryRowContext(ctx, query, walletID)
	return scanWallet(row)
}

// UpdateBalance persists the wallet balance.
func (r *WalletRepository) UpdateBalance(ctx context.Context, wallet *domain.Wallet) error {
	exec := dbOrTx(ctx, r.db)
	const query = `UPDATE wallets SET balance = $1 WHERE wallet_id = $2`

	result, err := exec.ExecContext(ctx, query, wallet.Balance, wallet.ID)
	if err != nil {
		return fmt.Errorf("update wallet balance: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("update wallet balance rows affected: %w", err)
	}
	if rows == 0 {
		return domain.ErrNotFound
	}
	return nil
}
