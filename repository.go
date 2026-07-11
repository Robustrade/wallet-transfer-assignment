package main

import (
	"context"
	"database/sql"
	"errors"
)

var ErrInsufficientFunds = errors.New("insufficient funds")

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return &Repository{db: db}
}

func (r *Repository) CreateWallet(id string, balance int64) error {
	_, err := r.db.Exec(`INSERT INTO wallets (id, balance) VALUES (?, ?)`, id, balance)
	return err
}

func (r *Repository) GetWalletBalance(id string) (int64, error) {
	var b int64
	err := r.db.QueryRow(`SELECT balance FROM wallets WHERE id = ?`, id).Scan(&b)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return b, err
}

// conditional debit: subtract amount if balance >= amount
func (r *Repository) DebitIfEnough(ctx context.Context, tx *sql.Tx, walletID string, amount int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance - ? WHERE id = ? AND balance >= ?`, amount, walletID, amount)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return ErrInsufficientFunds
	}
	return nil
}

func (r *Repository) Credit(ctx context.Context, tx *sql.Tx, walletID string, amount int64) error {
	res, err := tx.ExecContext(ctx, `UPDATE wallets SET balance = balance + ? WHERE id = ?`, amount, walletID)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) InsertTransfer(ctx context.Context, tx *sql.Tx, t Transfer) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, state) VALUES (?, ?, ?, ?, ?)`,
		t.ID, t.FromWalletID, t.ToWalletID, t.Amount, t.State)
	return err
}

func (r *Repository) UpdateTransferState(ctx context.Context, tx *sql.Tx, transferID, state string) error {
	_, err := tx.ExecContext(ctx, `UPDATE transfers SET state = ? WHERE id = ?`, state, transferID)
	return err
}

func (r *Repository) InsertLedgerEntry(ctx context.Context, tx *sql.Tx, e LedgerEntry) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount) VALUES (?, ?, ?, ?)`,
		e.WalletID, e.TransferID, e.Type, e.Amount)
	return err
}

func (r *Repository) InsertIdempotency(ctx context.Context, tx *sql.Tx, key string, status string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO idempotency_records (idempotency_key, status) VALUES (?, ?)`, key, status)
	return err
}

func (r *Repository) GetIdempotency(ctx context.Context, key string) (string, string, string, error) {
	var transferID sql.NullString
	var status string
	var response sql.NullString
	err := r.db.QueryRowContext(ctx, `SELECT transfer_id, status, response FROM idempotency_records WHERE idempotency_key = ?`, key).Scan(&transferID, &status, &response)
	if err != nil {
		return "", "", "", err
	}
	return transferID.String, status, response.String, nil
}

func (r *Repository) CompleteIdempotency(ctx context.Context, tx *sql.Tx, key, transferID, status, response string) error {
	_, err := tx.ExecContext(ctx, `UPDATE idempotency_records SET transfer_id = ?, status = ?, response = ? WHERE idempotency_key = ?`, transferID, status, response, key)
	return err
}
