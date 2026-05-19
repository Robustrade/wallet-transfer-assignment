package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/mayankkandpal18/wallet-transfer-assignment/internal/domain"
)

// WalletRepository reads and writes the wallets table.
type WalletRepository struct{}

func NewWalletRepository() *WalletRepository { return &WalletRepository{} }

// GetForUpdate reads a wallet and acquires a row-level write lock
// (SELECT ... FOR UPDATE). Callers must run this inside a transaction; the
// lock is held until that transaction commits or rolls back.
func (r *WalletRepository) GetForUpdate(ctx context.Context, db DBTX, id string) (domain.Wallet, error) {
	const q = `SELECT id, name, balance, created_at FROM wallets WHERE id = $1 FOR UPDATE`
	return scanWallet(db.QueryRow(ctx, q, id))
}

// GetByID reads a wallet without locking.
func (r *WalletRepository) GetByID(ctx context.Context, db DBTX, id string) (domain.Wallet, error) {
	const q = `SELECT id, name, balance, created_at FROM wallets WHERE id = $1`
	return scanWallet(db.QueryRow(ctx, q, id))
}

// AddToBalance applies a signed delta to a wallet's cached balance
// (balance = balance + delta). Direction (debit/credit) is the caller's
// concern; this is a neutral persistence operation.
func (r *WalletRepository) AddToBalance(ctx context.Context, db DBTX, id string, delta decimal.Decimal) error {
	const q = `UPDATE wallets SET balance = balance + $2 WHERE id = $1`
	ct, err := db.Exec(ctx, q, id, delta)
	if err != nil {
		return fmt.Errorf("add to balance: %w", err)
	}
	if ct.RowsAffected() == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func scanWallet(row pgx.Row) (domain.Wallet, error) {
	var w domain.Wallet
	err := row.Scan(&w.ID, &w.Name, &w.Balance, &w.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	if err != nil {
		return domain.Wallet{}, fmt.Errorf("scan wallet: %w", err)
	}
	return w, nil
}
