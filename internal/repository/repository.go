package repository

import (
	"context"
	"database/sql"
	"fmt"
)

type Repository struct {
	DB *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{DB: db}
}

// Lock wallet
func (r *Repository) GetWalletForUpdate(ctx context.Context, tx *sql.Tx, id string) (float64, error) {
	var balance float64
	err := tx.QueryRowContext(ctx,
		`SELECT balance FROM wallets WHERE id=$1 FOR UPDATE`,
		id,
	).Scan(&balance)

	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("wallet not found")
	}
	if err != nil {
		return 0, err
	}

	return balance, err
}

func (r *Repository) UpdateBalance(ctx context.Context, tx *sql.Tx, id string, delta float64) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE wallets SET balance = balance + $1 , updated_at=now() WHERE id=$2`,
		delta, id,
	)
	return err
}

func (r *Repository) CreateTransfer(ctx context.Context, tx *sql.Tx, tID, from, to string, amount float64, status string, key string) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO transfers (id, from_wallet_id, to_wallet_id, amount, status, idempotency_key)
         VALUES ($1,$2,$3,$4,$5,$6)`,
		tID, from, to, amount, status, key,
	)
	return err
}

func (r *Repository) UpdateTransferStatus(ctx context.Context, tx *sql.Tx, id string, status string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE transfers SET status=$1, updated_at=now()  WHERE id=$2`,
		status, id,
	)
	return err
}

func (r *Repository) CreateLedgerEntry(ctx context.Context, tx *sql.Tx, walletID, transferID, typ string, amount float64) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
         VALUES ($1,$2,$3,$4)`,
		walletID, transferID, typ, amount,
	)
	return err
}

// Idempotency
func (r *Repository) GetIdempotency(ctx context.Context, tx *sql.Tx, key string) ([]byte, error) {
	var resp []byte
	err := tx.QueryRowContext(ctx,
		`SELECT response FROM idempotency_records WHERE idempotency_key=$1`,
		key,
	).Scan(&resp)

	if err == sql.ErrNoRows {
		return nil, sql.ErrNoRows
	}
	if err != nil {
		return nil, err
	}

	return resp, err
}

func (r *Repository) InsertIdempotency(ctx context.Context, tx *sql.Tx, key, hash string, body []byte) (bool, error) {
	res, err := tx.ExecContext(ctx,
		`INSERT INTO idempotency_records 
		(idempotency_key, request_hash, request_body)
		VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`,
		key, hash, body,
	)
	if err != nil {
		return false, err
	}

	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}

	// true = first time insert
	// false = duplicate request
	return rows == 1, nil
}

func (r *Repository) UpdateIdempotency(ctx context.Context, tx *sql.Tx, key string, resp []byte) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE idempotency_records SET response=$1 WHERE idempotency_key=$2`,
		resp, key,
	)
	return err
}

func (r *Repository) GetRequestHash(ctx context.Context, tx *sql.Tx, key string) (string, error) {
	var hash string
	err := tx.QueryRowContext(ctx,
		`SELECT request_hash FROM idempotency_records WHERE idempotency_key=$1`,
		key,
	).Scan(&hash)

	return hash, err
}
