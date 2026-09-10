package repository

import (
	"context"
	"database/sql"
	"time"

	"github.com/ankurnitt1311/wallet-transfer-assignment/internal/domain"
)

const walletSelect = `SELECT id, balance, opening_balance, version, created_at FROM wallets`

func (t *Tx) InsertWallet(ctx context.Context, w domain.Wallet) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO wallets (id, balance, opening_balance, version, created_at)
		VALUES (?, ?, ?, ?, ?)`,
		w.ID, w.Balance, w.OpeningBalance, w.Version, w.CreatedAt.UTC().Format(timeLayout),
	)
	if IsUniqueViolation(err) {
		return domain.ErrWalletExists
	}
	return err
}

func (t *Tx) GetWallet(ctx context.Context, id string) (domain.Wallet, error) {
	return scanWallet(t.tx.QueryRowContext(ctx, walletSelect+" WHERE id = ?", id))
}

// Touch takes a write lock on the wallet row without changing balances.
// Callers lock wallets in sorted id order before debiting.
func (t *Tx) Touch(ctx context.Context, walletID string) error {
	res, err := t.tx.ExecContext(ctx, `UPDATE wallets SET version = version WHERE id = ?`, walletID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrWalletNotFound
	}
	return nil
}

// Debit subtracts amount only when the stored balance covers it.
// Zero rows means a concurrent debit won or the wallet is short.
func (t *Tx) Debit(ctx context.Context, walletID string, amount int64) (int64, error) {
	res, err := t.tx.ExecContext(ctx, `
		UPDATE wallets
		SET balance = balance - ?, version = version + 1
		WHERE id = ? AND balance >= ?`,
		amount, walletID, amount,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, domain.ErrInsufficientFunds
	}
	var balance int64
	if err := t.tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = ?`, walletID).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, nil
}

func (t *Tx) Credit(ctx context.Context, walletID string, amount int64) (int64, error) {
	res, err := t.tx.ExecContext(ctx, `
		UPDATE wallets
		SET balance = balance + ?, version = version + 1
		WHERE id = ?`,
		amount, walletID,
	)
	if err != nil {
		return 0, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, domain.ErrWalletNotFound
	}
	var balance int64
	if err := t.tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = ?`, walletID).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, nil
}

func scanWallet(row *sql.Row) (domain.Wallet, error) {
	var w domain.Wallet
	var created string
	err := row.Scan(&w.ID, &w.Balance, &w.OpeningBalance, &w.Version, &created)
	if err != nil {
		return domain.Wallet{}, mapNotFound(err, domain.ErrWalletNotFound)
	}
	w.CreatedAt, err = time.Parse(timeLayout, created)
	if err != nil {
		return domain.Wallet{}, err
	}
	return w, nil
}
