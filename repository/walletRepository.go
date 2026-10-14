package repository

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

var (
	ErrWalletNotFound   = errors.New("wallet not found")
	ErrInsufficientFund = errors.New("insufficient balance")
)

type Wallet struct {
	ID      string
	Balance int64
}

type Transfer struct {
	ID             string
	IdempotencyKey string
	FromWalletID   string
	ToWalletID     string
	Amount         int64
	Status         string
}

type WalletRepository struct {
	db *sql.DB
}

func NewWalletRepository(db *sql.DB) *WalletRepository {
	return &WalletRepository{db: db}
}

// ExecInTx executes a function inside a database transaction.
func (r *WalletRepository) ExecInTx(ctx context.Context, fn func(tx *sql.Tx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}

	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}

	return tx.Commit()
}

// GetTransferByIdempotencyKey checks if a transfer with the key already exists.
func (r *WalletRepository) GetTransferByIdempotencyKey(ctx context.Context, tx *sql.Tx, key string) (*Transfer, error) {
	query := `SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, status FROM transfers WHERE idempotency_key = ?`
	row := tx.QueryRowContext(ctx, query, key)

	var t Transfer
	err := row.Scan(&t.ID, &t.IdempotencyKey, &t.FromWalletID, &t.ToWalletID, &t.Amount, &t.Status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}

// GetWalletForUpdate retrieves a wallet row for balance updates inside a transaction.
func (r *WalletRepository) GetWalletForUpdate(ctx context.Context, tx *sql.Tx, walletID string) (*Wallet, error) {
	// Removed FOR UPDATE since SQLite handles file-level locking natively in transactions
	query := `SELECT id, balance FROM wallets WHERE id = ?`
	row := tx.QueryRowContext(ctx, query, walletID)

	var w Wallet
	err := row.Scan(&w.ID, &w.Balance)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrWalletNotFound
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

// UpdateWalletBalance updates a wallet's balance inside the transaction.
func (r *WalletRepository) UpdateWalletBalance(ctx context.Context, tx *sql.Tx, walletID string, newBalance int64) error {
	query := `UPDATE wallets SET balance = ? WHERE id = ?`
	_, err := tx.ExecContext(ctx, query, newBalance, walletID)
	return err
}

// CreateTransfer creates a transfer record.
func (r *WalletRepository) CreateTransfer(ctx context.Context, tx *sql.Tx, t *Transfer) error {
	query := `INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, status) VALUES (?, ?, ?, ?, ?, ?)`
	_, err := tx.ExecContext(ctx, query, t.ID, t.IdempotencyKey, t.FromWalletID, t.ToWalletID, t.Amount, t.Status)
	return err
}

// CreateLedgerEntry adds double-entry ledger records (DEBIT/CREDIT).
func (r *WalletRepository) CreateLedgerEntry(ctx context.Context, tx *sql.Tx, transferID, walletID, entryType string, amount int64) error {
	query := `INSERT INTO ledger_entries (id, transfer_id, wallet_id, type, amount) VALUES (?, ?, ?, ?, ?)`
	id := fmt.Sprintf("%s-%s", transferID, entryType)
	_, err := tx.ExecContext(ctx, query, id, transferID, walletID, entryType, amount)
	return err
}
