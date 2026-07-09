package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/domain"
	"github.com/vardaanmittal09/wallet-transfer-assignment/internal/port"
)

type Store struct {
	db *sql.DB
}

type Tx struct {
	tx *sql.Tx
}

func NewStore(db *sql.DB) *Store {
	db.SetMaxOpenConns(1)
	return &Store{db: db}
}

func DSN(path string) string {
	if strings.Contains(path, "?") {
		return path + "&_busy_timeout=5000&_foreign_keys=on"
	}
	return path + "?_busy_timeout=5000&_foreign_keys=on"
}

func (s *Store) Migrate(ctx context.Context) error {
	statements := []string{
		`PRAGMA foreign_keys = ON;`,
		`CREATE TABLE IF NOT EXISTS wallets (
			id TEXT PRIMARY KEY,
			balance INTEGER NOT NULL CHECK (balance >= 0),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
		);`,
		`CREATE TABLE IF NOT EXISTS transfers (
			id TEXT PRIMARY KEY,
			idempotency_key TEXT NOT NULL UNIQUE,
			from_wallet_id TEXT NOT NULL,
			to_wallet_id TEXT NOT NULL,
			amount INTEGER NOT NULL CHECK (amount > 0),
			state TEXT NOT NULL CHECK (state IN ('PENDING', 'PROCESSED', 'FAILED')),
			failure_reason TEXT NOT NULL DEFAULT '',
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			CHECK (from_wallet_id <> to_wallet_id)
		);`,
		`CREATE TABLE IF NOT EXISTS ledger_entries (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			wallet_id TEXT NOT NULL,
			transfer_id TEXT NOT NULL,
			type TEXT NOT NULL CHECK (type IN ('DEBIT', 'CREDIT')),
			amount INTEGER NOT NULL CHECK (amount > 0),
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (wallet_id) REFERENCES wallets(id),
			FOREIGN KEY (transfer_id) REFERENCES transfers(id),
			UNIQUE (transfer_id, type)
		);`,
		`CREATE TABLE IF NOT EXISTS idempotency_records (
			key TEXT PRIMARY KEY,
			request_hash TEXT NOT NULL,
			transfer_id TEXT NOT NULL UNIQUE,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (transfer_id) REFERENCES transfers(id)
		);`,
		`CREATE INDEX IF NOT EXISTS idx_ledger_entries_transfer_id ON ledger_entries(transfer_id);`,
		`CREATE INDEX IF NOT EXISTS idx_ledger_entries_wallet_id ON ledger_entries(wallet_id);`,
	}

	for _, statement := range statements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

func (s *Store) SeedWallets(ctx context.Context, wallets map[string]int64) error {
	for id, balance := range wallets {
		_, err := s.db.ExecContext(
			ctx,
			`INSERT INTO wallets (id, balance) VALUES (?, ?)
			 ON CONFLICT(id) DO NOTHING`,
			id,
			balance,
		)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	return scanWallet(s.db.QueryRowContext(ctx, `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = ?`, walletID))
}

func (s *Store) GetTransfer(ctx context.Context, transferID string) (domain.Transfer, error) {
	return scanTransfer(s.db.QueryRowContext(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = ?`, transferID))
}

func (s *Store) GetLedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return queryLedgerEntries(ctx, s.db, transferID)
}

func (s *Store) WithinTransfer(ctx context.Context, fn func(ctx context.Context, tx port.TransferTx) error) error {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return err
	}
	if err := fn(ctx, &Tx{tx: tx}); err != nil {
		if rollbackErr := tx.Rollback(); rollbackErr != nil {
			return fmt.Errorf("%w; rollback: %v", err, rollbackErr)
		}
		return err
	}
	return tx.Commit()
}

func (t *Tx) GetIdempotencyRecord(ctx context.Context, key string) (domain.IdempotencyRecord, bool, error) {
	var record domain.IdempotencyRecord
	err := t.tx.QueryRowContext(ctx, `
		SELECT key, request_hash, transfer_id, created_at
		FROM idempotency_records
		WHERE key = ?`, key).
		Scan(&record.Key, &record.RequestHash, &record.TransferID, &record.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.IdempotencyRecord{}, false, nil
	}
	return record, err == nil, err
}

func (t *Tx) InsertIdempotencyRecord(ctx context.Context, record domain.IdempotencyRecord) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO idempotency_records (key, request_hash, transfer_id)
		VALUES (?, ?, ?)`,
		record.Key,
		record.RequestHash,
		record.TransferID,
	)
	return err
}

func (t *Tx) InsertTransfer(ctx context.Context, transfer domain.Transfer) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO transfers (id, idempotency_key, from_wallet_id, to_wallet_id, amount, state)
		VALUES (?, ?, ?, ?, ?, ?)`,
		transfer.ID,
		transfer.IdempotencyKey,
		transfer.FromWalletID,
		transfer.ToWalletID,
		transfer.Amount,
		transfer.State,
	)
	return err
}

func (t *Tx) GetTransfer(ctx context.Context, transferID string) (domain.Transfer, error) {
	return scanTransfer(t.tx.QueryRowContext(ctx, `
		SELECT id, idempotency_key, from_wallet_id, to_wallet_id, amount, state, failure_reason, created_at, updated_at
		FROM transfers
		WHERE id = ?`, transferID))
}

func (t *Tx) GetWallet(ctx context.Context, walletID string) (domain.Wallet, error) {
	return scanWallet(t.tx.QueryRowContext(ctx, `SELECT id, balance, created_at, updated_at FROM wallets WHERE id = ?`, walletID))
}

func (t *Tx) DebitWallet(ctx context.Context, walletID string, amount int64) (bool, error) {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE wallets
		SET balance = balance - ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND balance >= ?`,
		amount,
		walletID,
		amount,
	)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	return rows == 1, err
}

func (t *Tx) CreditWallet(ctx context.Context, walletID string, amount int64) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE wallets
		SET balance = balance + ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?`,
		amount,
		walletID,
	)
	if err != nil {
		return err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return domain.ErrWalletNotFound
	}
	return nil
}

func (t *Tx) InsertLedgerEntry(ctx context.Context, entry domain.LedgerEntry) error {
	_, err := t.tx.ExecContext(ctx, `
		INSERT INTO ledger_entries (wallet_id, transfer_id, type, amount)
		VALUES (?, ?, ?, ?)`,
		entry.WalletID,
		entry.TransferID,
		entry.Type,
		entry.Amount,
	)
	return err
}

func (t *Tx) MarkTransferProcessed(ctx context.Context, transferID string) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE transfers
		SET state = 'PROCESSED', updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND state = 'PENDING'`,
		transferID,
	)
	if err != nil {
		return err
	}
	return requireOneRow(result, domain.ErrInvalidTransferTransition)
}

func (t *Tx) MarkTransferFailed(ctx context.Context, transferID string, reason string) error {
	result, err := t.tx.ExecContext(ctx, `
		UPDATE transfers
		SET state = 'FAILED', failure_reason = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND state = 'PENDING'`,
		reason,
		transferID,
	)
	if err != nil {
		return err
	}
	return requireOneRow(result, domain.ErrInvalidTransferTransition)
}

func (t *Tx) GetLedgerEntries(ctx context.Context, transferID string) ([]domain.LedgerEntry, error) {
	return queryLedgerEntries(ctx, t.tx, transferID)
}

func scanWallet(row *sql.Row) (domain.Wallet, error) {
	var wallet domain.Wallet
	err := row.Scan(&wallet.ID, &wallet.Balance, &wallet.CreatedAt, &wallet.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Wallet{}, domain.ErrWalletNotFound
	}
	return wallet, err
}

func scanTransfer(row *sql.Row) (domain.Transfer, error) {
	var transfer domain.Transfer
	err := row.Scan(
		&transfer.ID,
		&transfer.IdempotencyKey,
		&transfer.FromWalletID,
		&transfer.ToWalletID,
		&transfer.Amount,
		&transfer.State,
		&transfer.FailureReason,
		&transfer.CreatedAt,
		&transfer.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Transfer{}, domain.ErrTransferNotFound
	}
	return transfer, err
}

type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

func queryLedgerEntries(ctx context.Context, q queryer, transferID string) ([]domain.LedgerEntry, error) {
	rows, err := q.QueryContext(ctx, `
		SELECT id, wallet_id, transfer_id, type, amount, created_at
		FROM ledger_entries
		WHERE transfer_id = ?
		ORDER BY id`, transferID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []domain.LedgerEntry
	for rows.Next() {
		var entry domain.LedgerEntry
		if err := rows.Scan(&entry.ID, &entry.WalletID, &entry.TransferID, &entry.Type, &entry.Amount, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}

func requireOneRow(result sql.Result, errWhenMissing error) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if rows != 1 {
		return errWhenMissing
	}
	return nil
}
