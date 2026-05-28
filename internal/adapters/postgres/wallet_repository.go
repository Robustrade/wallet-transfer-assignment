package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"wallet-transfer-assignment/internal/wallets"
)

type WalletRepository struct {
	pool *pgxpool.Pool
}

func NewWalletRepository(pool *pgxpool.Pool) *WalletRepository {
	return &WalletRepository{pool: pool}
}

func (r *WalletRepository) Create(ctx context.Context, initialBalance int64) (wallets.Wallet, error) {
	id := uuid.NewString()
	_, err := r.pool.Exec(ctx,
		`INSERT INTO wallets (wallet_id, balance) VALUES ($1, $2)`,
		id, initialBalance,
	)
	if err != nil {
		return wallets.Wallet{}, err
	}
	return wallets.Wallet{ID: id, Balance: initialBalance}, nil
}

func (r *WalletRepository) Get(ctx context.Context, walletID string) (wallets.Wallet, error) {
	var bal int64
	err := r.pool.QueryRow(ctx, `SELECT balance FROM wallets WHERE wallet_id = $1`, walletID).Scan(&bal)
	if isNoRows(err) {
		return wallets.Wallet{}, wallets.ErrNotFound
	}
	if err != nil {
		return wallets.Wallet{}, err
	}
	return wallets.Wallet{ID: walletID, Balance: bal}, nil
}

func (r *WalletRepository) Exists(ctx context.Context, walletID string) (bool, error) {
	var exists bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM wallets WHERE wallet_id = $1)`, walletID).Scan(&exists)
	return exists, err
}

// Tx helpers for transfer workflow.

func (r *WalletRepository) LockTx(ctx context.Context, tx pgx.Tx, walletID string) (bool, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT wallet_id FROM wallets WHERE wallet_id = $1 FOR UPDATE`, walletID).Scan(&id)
	if isNoRows(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *WalletRepository) DebitIfSufficientTx(ctx context.Context, tx pgx.Tx, walletID string, amount int64) (bool, error) {
	tag, err := tx.Exec(ctx,
		`UPDATE wallets
         SET balance = balance - $2, updated_at = now()
         WHERE wallet_id = $1 AND balance >= $2`,
		walletID, amount,
	)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() == 1, nil
}

func (r *WalletRepository) CreditTx(ctx context.Context, tx pgx.Tx, walletID string, amount int64) error {
	tag, err := tx.Exec(ctx,
		`UPDATE wallets
         SET balance = balance + $2, updated_at = now()
         WHERE wallet_id = $1`,
		walletID, amount,
	)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("credit affected %d rows", tag.RowsAffected())
	}
	return nil
}
