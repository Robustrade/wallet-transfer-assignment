package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "github.com/mattn/go-sqlite3"

	"wallet-transfer-assignment/internal/domain"
	"wallet-transfer-assignment/internal/service"
)

type SQLiteRepository struct {
	db *sql.DB
}

type Options struct {
	MaxOpenConns int
	MaxIdleConns int
	EnableWAL    bool
}

func Open(ctx context.Context, dsn string) (*SQLiteRepository, error) {
	return OpenWithOptions(ctx, dsn, Options{
		MaxOpenConns: 4,
		MaxIdleConns: 4,
		EnableWAL:    true,
	})
}

func OpenWithOptions(ctx context.Context, dsn string, options Options) (*SQLiteRepository, error) {
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	if options.MaxOpenConns <= 0 {
		options.MaxOpenConns = 4
	}
	if options.MaxIdleConns < 0 {
		options.MaxIdleConns = 0
	}
	db.SetMaxOpenConns(options.MaxOpenConns)
	db.SetMaxIdleConns(options.MaxIdleConns)

	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout = 5000;"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if options.EnableWAL {
		if _, err := db.ExecContext(ctx, "PRAGMA journal_mode = WAL;"); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &SQLiteRepository{db: db}, nil
}

func (r *SQLiteRepository) Close() error {
	return r.db.Close()
}

func (r *SQLiteRepository) DB() *sql.DB {
	return r.db
}

func (r *SQLiteRepository) WithinTx(ctx context.Context, fn func(context.Context, service.TransferTx) error) error {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	wrapped := &transferTx{tx: tx}
	if err := fn(ctx, wrapped); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil && !errors.Is(rollbackErr, sql.ErrTxDone) {
			return fmt.Errorf("%w; rollback failed: %v", err, rollbackErr)
		}
		return err
	}
	return tx.Commit()
}

func (r *SQLiteRepository) CreateWallet(ctx context.Context, id string, balance int64) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO wallets (id, balance)
		VALUES (?, ?)
	`, id, balance)
	return err
}

func (r *SQLiteRepository) WalletBalance(ctx context.Context, id string) (int64, bool, error) {
	var balance int64
	err := r.db.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = ?`, id).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return balance, true, nil
}

func (r *SQLiteRepository) LedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, wallet_id, transfer_id, type, amount
		FROM ledger_entries
		WHERE transfer_id = ?
		ORDER BY type DESC
	`, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var entry domain.LedgerEntry
		if err := rows.Scan(&entry.ID, &entry.WalletID, &entry.TransferID, &entry.Type, &entry.Amount); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func (r *SQLiteRepository) TransferState(ctx context.Context, transferID string) (domain.TransferState, bool, error) {
	var state domain.TransferState
	err := r.db.QueryRowContext(ctx, `SELECT state FROM transfers WHERE id = ?`, transferID).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return state, true, nil
}

type transferTx struct {
	tx *sql.Tx
}

func (t *transferTx) FindOrCreateIdempotency(ctx context.Context, key string, requestHash string) (service.IdempotencyRecord, bool, error) {
	result, err := t.tx.ExecContext(ctx, `
		INSERT INTO idempotency_records (idempotency_key, request_hash)
		VALUES (?, ?)
		ON CONFLICT(idempotency_key) DO NOTHING
	`, key, requestHash)
	if err != nil {
		return service.IdempotencyRecord{}, false, err
	}
	createdRows, err := result.RowsAffected()
	if err != nil {
		return service.IdempotencyRecord{}, false, err
	}

	var record service.IdempotencyRecord
	var completed int
	err = t.tx.QueryRowContext(ctx, `
		SELECT idempotency_key, request_hash, COALESCE(response_code, 0), COALESCE(response_body, X''), completed
		FROM idempotency_records
		WHERE idempotency_key = ?
	`, key).Scan(&record.Key, &record.RequestHash, &record.ResponseCode, &record.ResponseBody, &completed)
	if err != nil {
		return service.IdempotencyRecord{}, false, err
	}
	record.Completed = completed == 1
	return record, createdRows == 1, nil
}

func (t *transferTx) CompleteIdempotency(ctx context.Context, key string, outcome domain.Outcome) error {
	body, err := json.Marshal(outcome.Body)
	if err != nil {
		return err
	}
	_, err = t.tx.ExecContext(ctx, `
		UPDATE idempotency_records
		SET response_code = ?, response_body = ?, completed = 1, updated_at = CURRENT_TIMESTAMP
		WHERE idempotency_key = ?
	`, outcome.StatusCode, body, key)
	return err
}

func (t *transferTx) GetWallet(ctx context.Context, walletID string) (service.Wallet, bool, error) {
	var wallet service.Wallet
	err := t.tx.QueryRowContext(ctx, `
		SELECT id, balance
		FROM wallets
		WHERE id = ?
	`, walletID).Scan(&wallet.ID, &wallet.Balance)
	if errors.Is(err, sql.ErrNoRows) {
		return service.Wallet{}, false, nil
	}
	if err != nil {
		return service.Wallet{}, false, err
	}
	return wallet, true, nil
}

func (t *transferTx) CreateTransfer(ctx context.Context, transfer domain.Transfer) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, failure_reason)
		VALUES (?, ?, ?, ?, ?, ?, ?)
	`, transfer.ID, transfer.IdempotencyKey, transfer.FromWalletID, transfer.ToWalletID, transfer.Amount, transfer.State, transfer.FailureReason)
	return err
}

func (t *transferTx) SetTransferState(ctx context.Context, transferID string, state domain.TransferState, failureReason string) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE transfers
		SET state = ?,
		    failure_reason = ?,
		    processed_at = CASE WHEN ? IN ('PROCESSED', 'FAILED') THEN CURRENT_TIMESTAMP ELSE processed_at END
		WHERE id = ?
		  AND state = 'PENDING'
	`, state, failureReason, state, transferID)
	if err != nil {
		return err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rowsAffected != 1 {
		return fmt.Errorf("transfer %q could not transition to %s", transferID, state)
	}
	return nil
}

func (t *transferTx) DebitWallet(ctx context.Context, walletID string, amount int64) (int64, bool, error) {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE wallets
		SET balance = balance - ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
		  AND balance >= ?
	`, amount, walletID, amount)
	if err != nil {
		return 0, false, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, false, err
	}
	if rowsAffected == 0 {
		return 0, false, nil
	}

	var balance int64
	if err := t.tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = ?`, walletID).Scan(&balance); err != nil {
		return 0, false, err
	}
	return balance, true, nil
}

func (t *transferTx) CreditWallet(ctx context.Context, walletID string, amount int64) (int64, error) {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE wallets
		SET balance = balance + ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`, amount, walletID)
	if err != nil {
		return 0, err
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if rowsAffected != 1 {
		return 0, fmt.Errorf("credit wallet %q affected %d rows", walletID, rowsAffected)
	}

	var balance int64
	if err := t.tx.QueryRowContext(ctx, `SELECT balance FROM wallets WHERE id = ?`, walletID).Scan(&balance); err != nil {
		return 0, err
	}
	return balance, nil
}

func (t *transferTx) CreateLedgerEntries(ctx context.Context, entries []domain.LedgerEntry) error {
	stmt, err := t.tx.PrepareContext(ctx, `
		INSERT INTO ledger_entries (id, wallet_id, transfer_id, type, amount)
		VALUES (?, ?, ?, ?, ?)
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, entry := range entries {
		if _, err := stmt.ExecContext(ctx, entry.ID, entry.WalletID, entry.TransferID, entry.Type, entry.Amount); err != nil {
			return err
		}
	}
	return nil
}
