package repository

import (
	"context"
	"database/sql"
	"fmt"
	"wallet-transfer/internal/transfer/domain"
	walletdomain "wallet-transfer/internal/wallet/domain"

	"github.com/google/uuid"
)

type Repository struct{ db *sql.DB }

func New(db *sql.DB) *Repository { return &Repository{db: db} }
func (r *Repository) Create(ctx context.Context, request walletdomain.CreateRequest) (walletdomain.Wallet, error) {
	var w walletdomain.Wallet
	currency := request.Currency
	if currency == "" {
		currency = "USD"
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return w, fmt.Errorf("begin wallet creation: %w", err)
	}
	defer tx.Rollback()
	err = tx.QueryRowContext(ctx, `INSERT INTO wallets (id,name,balance,currency) VALUES ($1,$2,$3,$4) RETURNING id,name,balance,currency,created_at,updated_at`, uuid.NewString(), request.Name, request.InitialBalance, currency).Scan(&w.ID, &w.Name, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return w, fmt.Errorf("create wallet: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wallet_balance_history (wallet_id, balance, change, reason) VALUES ($1, $2, $2, $3)`, w.ID, w.Balance, "INITIAL_BALANCE"); err != nil {
		return w, fmt.Errorf("record initial balance: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return w, fmt.Errorf("commit wallet creation: %w", err)
	}
	return w, nil
}
func (r *Repository) Get(ctx context.Context, id string) (walletdomain.Wallet, error) {
	var w walletdomain.Wallet
	err := r.db.QueryRowContext(ctx, `SELECT id,name,balance,currency,created_at,updated_at FROM wallets WHERE id=$1`, id).Scan(&w.ID, &w.Name, &w.Balance, &w.Currency, &w.CreatedAt, &w.UpdatedAt)
	if err == sql.ErrNoRows {
		return w, walletdomain.ErrNotFound
	}
	if err != nil {
		return w, fmt.Errorf("get wallet: %w", err)
	}
	return w, nil
}

func (r *Repository) List(ctx context.Context) ([]walletdomain.Wallet, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, balance, currency, created_at, updated_at FROM wallets ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("list wallets: %w", err)
	}
	defer rows.Close()
	wallets := make([]walletdomain.Wallet, 0)
	for rows.Next() {
		var wallet walletdomain.Wallet
		if err := rows.Scan(&wallet.ID, &wallet.Name, &wallet.Balance, &wallet.Currency, &wallet.CreatedAt, &wallet.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan wallet: %w", err)
		}
		wallets = append(wallets, wallet)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate wallets: %w", err)
	}
	return wallets, nil
}

func (r *Repository) UpdateBalance(ctx context.Context, id string, balance int64) (walletdomain.Wallet, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return walletdomain.Wallet{}, fmt.Errorf("begin balance update: %w", err)
	}
	defer tx.Rollback()

	var current walletdomain.Wallet
	err = tx.QueryRowContext(ctx, `SELECT id, name, balance, currency FROM wallets WHERE id = $1 FOR UPDATE`, id).Scan(&current.ID, &current.Name, &current.Balance, &current.Currency)
	if err == sql.ErrNoRows {
		return walletdomain.Wallet{}, walletdomain.ErrNotFound
	}
	if err != nil {
		return walletdomain.Wallet{}, fmt.Errorf("lock wallet for balance update: %w", err)
	}
	var updated walletdomain.Wallet
	err = tx.QueryRowContext(ctx, `UPDATE wallets SET balance = $1, version = version + 1, updated_at = NOW() WHERE id = $2 RETURNING id, name, balance, currency, created_at, updated_at`, balance, id).Scan(&updated.ID, &updated.Name, &updated.Balance, &updated.Currency, &updated.CreatedAt, &updated.UpdatedAt)
	if err != nil {
		return walletdomain.Wallet{}, fmt.Errorf("update wallet balance: %w", err)
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO wallet_balance_history (wallet_id, balance, change, reason) VALUES ($1, $2, $2::BIGINT - $3::BIGINT, $4)`, id, updated.Balance, current.Balance, "BALANCE_UPDATE"); err != nil {
		return walletdomain.Wallet{}, fmt.Errorf("record balance history: %w", err)
	}
	if err = tx.Commit(); err != nil {
		return walletdomain.Wallet{}, fmt.Errorf("commit balance update: %w", err)
	}
	return updated, nil
}

func (r *Repository) BalanceHistory(ctx context.Context, id string) ([]walletdomain.BalanceHistory, error) {
	if _, err := r.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, wallet_id, balance, change, reason, created_at FROM wallet_balance_history WHERE wallet_id = $1 ORDER BY created_at DESC, id DESC`, id)
	if err != nil {
		return nil, fmt.Errorf("list balance history: %w", err)
	}
	defer rows.Close()
	history := make([]walletdomain.BalanceHistory, 0)
	for rows.Next() {
		var item walletdomain.BalanceHistory
		if err := rows.Scan(&item.ID, &item.WalletID, &item.Balance, &item.Change, &item.Reason, &item.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan balance history: %w", err)
		}
		history = append(history, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate balance history: %w", err)
	}
	return history, nil
}
func (r *Repository) ListTransfers(ctx context.Context, id string) ([]domain.Transfer, error) {
	if _, err := r.Get(ctx, id); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,idempotency_key,from_wallet_id,to_wallet_id,amount,status,failure_reason,created_at,updated_at FROM transfers WHERE from_wallet_id=$1 OR to_wallet_id=$1 ORDER BY created_at DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scan(rows)
}
func scan(rows *sql.Rows) ([]domain.Transfer, error) {
	out := make([]domain.Transfer, 0)
	for rows.Next() {
		var t domain.Transfer
		var reason sql.NullString
		if err := rows.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status, &reason, &t.CreatedAt, &t.UpdatedAt); err != nil {
			return nil, err
		}
		t.FailureReason = reason.String
		out = append(out, t)
	}
	return out, rows.Err()
}
