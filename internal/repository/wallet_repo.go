package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/candidate/wallet-transfer/internal/db"
	"github.com/candidate/wallet-transfer/internal/domain"
	"github.com/shopspring/decimal"
)

type WalletRepository struct {
	db *db.DB
}

func NewWalletRepository(d *db.DB) *WalletRepository {
	return &WalletRepository{db: d}
}

func (r *WalletRepository) Create(ctx context.Context, userID uint64, currency string) (*domain.Wallet, error) {
	const q = `
		INSERT INTO wallets (user_id, currency, balance)
		VALUES (?, ?, 0.00000000)`

	res, err := r.db.ExecContext(ctx, q, userID, currency)
	if err != nil {
		return nil, fmt.Errorf("wallet create: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("wallet last insert id: %w", err)
	}

	return r.GetByID(ctx, r.db, uint64(id))
}

func (r *WalletRepository) GetByID(ctx context.Context, q db.Querier, id uint64) (*domain.Wallet, error) {
	const query = `
		SELECT id, user_id, currency, balance, created_at, updated_at
		FROM   wallets
		WHERE  id = ?`

	return scanWallet(q.QueryRowContext(ctx, query, id))
}

func (r *WalletRepository) LockByID(ctx context.Context, tx *sql.Tx, id uint64) (*domain.Wallet, error) {
	const query = `
		SELECT id, user_id, currency, balance, created_at, updated_at
		FROM   wallets
		WHERE  id = ?`

	return scanWallet(tx.QueryRowContext(ctx, query, id))
}

func (r *WalletRepository) UpdateBalance(ctx context.Context, tx *sql.Tx, id uint64, newBalance decimal.Decimal) error {
	const q = `UPDATE wallets SET balance = ?, updated_at = CURRENT_TIMESTAMP WHERE id = ?`

	res, err := tx.ExecContext(ctx, q, newBalance.String(), id)
	if err != nil {
		return fmt.Errorf("wallet update balance: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func scanWallet(row *sql.Row) (*domain.Wallet, error) {
	var w domain.Wallet
	var balance string

	err := row.Scan(
		&w.ID,
		&w.UserID,
		&w.Currency,
		&balance,
		&w.CreatedAt,
		&w.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, domain.ErrWalletNotFound
		}
		return nil, fmt.Errorf("scan wallet: %w", err)
	}

	w.Balance, err = decimal.NewFromString(balance)
	if err != nil {
		return nil, fmt.Errorf("parse balance: %w", err)
	}

	return &w, nil
}
