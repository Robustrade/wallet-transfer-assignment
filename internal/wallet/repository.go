package wallet

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type Repository interface {
	BeginTx(ctx context.Context) (*sql.Tx, error)
	GetWalletByIDForUpdate(ctx context.Context, tx *sql.Tx, walletID string) (*Wallet, error)
	FindTransferByIdempotencyKey(ctx context.Context, key string) (*Transfer, error)
	CreateTransfer(ctx context.Context, tx *sql.Tx, t *Transfer) error
	UpdateTransferStatus(ctx context.Context, tx *sql.Tx, id string, status TransactionStateType) error
	UpdateWalletBalance(ctx context.Context, tx *sql.Tx, walletID string, newBalance int64) error
	CreateLedgerEntries(ctx context.Context, tx *sql.Tx, entries []LedgerEntry) error
}

type pgRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) Repository {
	return &pgRepository{db: db}
}

func (r *pgRepository) BeginTx(ctx context.Context) (*sql.Tx, error) {
	return r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
}

func (r *pgRepository) GetWalletByIDForUpdate(ctx context.Context, tx *sql.Tx, walletID string) (*Wallet, error) {
	var w Wallet
	err := tx.QueryRowContext(ctx, `
		SELECT wallet_id, balance, status, created_at, updated_at
		FROM wallets
		WHERE wallet_id = $1
		FOR UPDATE
	`, walletID).Scan(&w.WalletId, &w.Balance, &w.Status, &w.CreatedAt, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (r *pgRepository) FindTransferByIdempotencyKey(ctx context.Context, key string) (*Transfer, error) {
	var t Transfer
	err := r.db.QueryRowContext(ctx, `
		SELECT transaction_id, idempotency_key, from_wallet_id, to_wallet_id, amount, status, created_at, updated_at
		FROM transfers
		WHERE idempotency_key = $1
	`, key).Scan(&t.TransactionId, &t.IdempotencyKey, &t.FromWalletId, &t.ToWalletId, &t.Amount, &t.State, &t.CreatedAt, &t.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func (r *pgRepository) CreateTransfer(ctx context.Context, tx *sql.Tx, t *Transfer) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO transfers (transaction_id, idempotency_key, from_wallet_id, to_wallet_id, amount, status)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, t.TransactionId, t.IdempotencyKey, t.FromWalletId, t.ToWalletId, t.Amount, t.State)
	return err
}

func (r *pgRepository) UpdateTransferStatus(ctx context.Context, tx *sql.Tx, id string, status TransactionStateType) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE transfers SET status = $1, updated_at = NOW() WHERE transaction_id = $2
	`, status, id)
	return err
}

func (r *pgRepository) UpdateWalletBalance(ctx context.Context, tx *sql.Tx, walletID string, newBalance int64) error {
	_, err := tx.ExecContext(ctx, `
		UPDATE wallets SET balance = $1, updated_at = NOW() WHERE wallet_id = $2
	`, newBalance, walletID)
	return err
}

func (r *pgRepository) CreateLedgerEntries(ctx context.Context, tx *sql.Tx, entries []LedgerEntry) error {
	if len(entries) == 0 {
		return nil
	}

	placeholders := make([]string, 0, len(entries))
	args := make([]any, 0, len(entries)*7)
	for i, e := range entries {
		base := i * 7
		placeholders = append(placeholders, fmt.Sprintf("($%d,$%d,$%d,$%d,$%d,$%d,$%d)",
			base+1, base+2, base+3, base+4, base+5, base+6, base+7))
		args = append(args, e.EntryId, e.TransactionId, e.WalletId, e.Amount, e.Entry, e.BalanceBefore, e.BalanceAfter)
	}

	query := `INSERT INTO ledger_entries (entry_id, transaction_id, wallet_id, amount, entry, balance_before, balance_after) VALUES ` +
		strings.Join(placeholders, ",")
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}
